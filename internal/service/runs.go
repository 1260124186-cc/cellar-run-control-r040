package service

import (
	"sort"

	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/domain"
)

func (s *Service) CreateRun(
	input CreateRunInput,
) (InstructionResult[domain.FermentationRun], error) {
	now := s.clock.Now()
	code := domain.NormalizeRunCode(input.Code)
	if code == "" {
		generated, err := generatedRunCode()
		if err != nil {
			return InstructionResult[domain.FermentationRun]{},
				domain.NewError(domain.CodeStorage, err.Error())
		}
		code = generated
	}
	run := domain.FermentationRun{
		Code:           code,
		FormulaID:      input.FormulaID,
		PlannedVolumeL: input.PlannedVolumeL,
		Notes:          input.Notes,
		State:          domain.RunPlanned,
		Observations:   []domain.GravityObservation{},
		Version:        1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := run.Validate(); err != nil {
		return InstructionResult[domain.FermentationRun]{}, err
	}
	id, err := generateEntityID("run")
	if err != nil {
		return InstructionResult[domain.FermentationRun]{},
			domain.NewError(domain.CodeStorage, err.Error())
	}
	run.ID = id

	return runInstruction(s, input.Instruction, 201,
		func(snapshot *domain.Snapshot) (domain.FermentationRun, error) {
			formula, ok := snapshot.Formulas[run.FormulaID]
			if !ok {
				return domain.FermentationRun{}, domain.NewError(domain.CodeFormulaNotFound,
					"formula was not found")
			}
			if formula.State != domain.FormulaApproved {
				return domain.FermentationRun{}, domain.NewError(domain.CodeFormulaState,
					"run requires an approved formula")
			}
			if _, exists := snapshot.RunIndex[run.Code]; exists {
				return domain.FermentationRun{}, domain.NewError(domain.CodeRunConflict,
					"a run with the same code already exists")
			}
			snapshot.Runs[run.ID] = run.Clone()
			snapshot.RunIndex[run.Code] = run.ID
			return run.Clone(), nil
		})
}

func (s *Service) ReserveRun(
	id string,
	input ReserveRunInput,
) (InstructionResult[RunVesselPayload], error) {
	return runInstruction(s, input.Instruction, 200,
		func(snapshot *domain.Snapshot) (RunVesselPayload, error) {
			run, ok := snapshot.Runs[id]
			if !ok {
				return RunVesselPayload{}, domain.NewError(domain.CodeRunNotFound,
					"run was not found")
			}
			if err := s.checkExpected(run.Version, input.ExpectedVersion); err != nil {
				return RunVesselPayload{}, err
			}
			vessel, ok := snapshot.Vessels[input.VesselID]
			if !ok {
				return RunVesselPayload{}, domain.NewError(domain.CodeVesselNotFound,
					"vessel was not found")
			}
			if run.PlannedVolumeL > vessel.CapacityL {
				return RunVesselPayload{}, domain.FieldError(domain.CodeInvalidInput,
					"planned_volume_liters", "exceeds vessel capacity")
			}
			if err := vessel.Reserve(run.ID, s.clock.Now()); err != nil {
				return RunVesselPayload{}, err
			}
			if err := run.ReserveVessel(vessel.ID, s.clock.Now()); err != nil {
				return RunVesselPayload{}, err
			}
			snapshot.Runs[id] = run.Clone()
			snapshot.Vessels[vessel.ID] = vessel
			return RunVesselPayload{Run: run.Clone(), Vessel: vessel}, nil
		})
}

func (s *Service) StartRun(
	id string,
	input VersionInput,
) (InstructionResult[RunVesselPayload], error) {
	return runInstruction(s, input.Instruction, 200,
		func(snapshot *domain.Snapshot) (RunVesselPayload, error) {
			run, ok := snapshot.Runs[id]
			if !ok {
				return RunVesselPayload{}, domain.NewError(domain.CodeRunNotFound,
					"run was not found")
			}
			if err := s.checkExpected(run.Version, input.ExpectedVersion); err != nil {
				return RunVesselPayload{}, err
			}
			if run.VesselID == nil {
				return RunVesselPayload{}, domain.NewError(domain.CodeRunState,
					"run has no vessel")
			}
			vessel, ok := snapshot.Vessels[*run.VesselID]
			if !ok {
				return RunVesselPayload{}, domain.NewError(domain.CodeVesselNotFound,
					"vessel was not found")
			}
			if err := vessel.StartRun(run.ID, s.clock.Now()); err != nil {
				return RunVesselPayload{}, err
			}
			if err := run.StartFermentation(s.clock.Now()); err != nil {
				return RunVesselPayload{}, err
			}
			snapshot.Runs[id] = run.Clone()
			snapshot.Vessels[vessel.ID] = vessel
			return RunVesselPayload{Run: run.Clone(), Vessel: vessel}, nil
		})
}

func (s *Service) AbortRun(
	id string,
	input VersionInput,
) (InstructionResult[RunVesselPayload], error) {
	return runInstruction(s, input.Instruction, 200,
		func(snapshot *domain.Snapshot) (RunVesselPayload, error) {
			run, ok := snapshot.Runs[id]
			if !ok {
				return RunVesselPayload{}, domain.NewError(domain.CodeRunNotFound,
					"run was not found")
			}
			if err := s.checkExpected(run.Version, input.ExpectedVersion); err != nil {
				return RunVesselPayload{}, err
			}
			hadStarted := run.HadStarted()
			if err := run.Abort(s.clock.Now()); err != nil {
				return RunVesselPayload{}, err
			}
			var vessel domain.Vessel
			if run.VesselID != nil {
				var found bool
				vessel, found = snapshot.Vessels[*run.VesselID]
				if !found {
					return RunVesselPayload{}, domain.NewError(domain.CodeVesselNotFound,
						"vessel was not found")
				}
				var err error
				if hadStarted {
					err = vessel.StartCleaning(run.ID, s.clock.Now())
				} else {
					err = vessel.ReleaseReservation(run.ID, s.clock.Now())
				}
				if err != nil {
					return RunVesselPayload{}, err
				}
				snapshot.Vessels[vessel.ID] = vessel
			}
			snapshot.Runs[id] = run.Clone()
			return RunVesselPayload{Run: run.Clone(), Vessel: vessel}, nil
		})
}

func (s *Service) GetRun(id string) (domain.FermentationRun, error) {
	var found domain.FermentationRun
	err := s.store.View(func(snapshot domain.Snapshot) error {
		run, ok := snapshot.Runs[id]
		if !ok {
			return domain.NewError(domain.CodeRunNotFound, "run was not found")
		}
		found = run.Clone()
		return nil
	})
	if err != nil {
		return domain.FermentationRun{}, err
	}
	return found, nil
}

func (s *Service) ListRuns() ([]domain.FermentationRun, error) {
	var runs []domain.FermentationRun
	err := s.store.View(func(snapshot domain.Snapshot) error {
		runs = make([]domain.FermentationRun, 0, len(snapshot.Runs))
		for _, run := range snapshot.Runs {
			runs = append(runs, run.Clone())
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].Code == runs[j].Code {
			return runs[i].ID < runs[j].ID
		}
		return runs[i].Code < runs[j].Code
	})
	return runs, nil
}
