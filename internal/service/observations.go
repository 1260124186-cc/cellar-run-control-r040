package service

import "github.com/1260124186-cc/solo-0016-cellar-run-control/internal/domain"

func (s *Service) AppendObservation(
	id string,
	input ObservationInput,
) (InstructionResult[domain.FermentationRun], error) {
	return runInstruction(s, input.Instruction, 200,
		func(snapshot *domain.Snapshot) (domain.FermentationRun, error) {
			run, ok := snapshot.Runs[id]
			if !ok {
				return domain.FermentationRun{}, domain.NewError(domain.CodeRunNotFound,
					"run was not found")
			}
			now := s.clock.Now()
			observation := domain.GravityObservation{
				RunID:        run.ID,
				Sequence:     input.Sequence,
				Gravity:      input.Gravity,
				TemperatureC: input.TemperatureC,
				ObservedAt:   input.ObservedAt.UTC(),
				Note:         input.Note,
				ReceivedAt:   now,
			}
			if err := run.AddObservation(observation); err != nil {
				return domain.FermentationRun{}, err
			}
			snapshot.Runs[id] = run.Clone()
			return run.Clone(), nil
		})
}

func (s *Service) BeginConditioning(
	id string,
	input VersionInput,
) (InstructionResult[domain.FermentationRun], error) {
	return runInstruction(s, input.Instruction, 200,
		func(snapshot *domain.Snapshot) (domain.FermentationRun, error) {
			run, ok := snapshot.Runs[id]
			if !ok {
				return domain.FermentationRun{}, domain.NewError(domain.CodeRunNotFound,
					"run was not found")
			}
			if err := s.checkExpected(run.Version, input.ExpectedVersion); err != nil {
				return domain.FermentationRun{}, err
			}
			if err := run.BeginConditioning(s.clock.Now()); err != nil {
				return domain.FermentationRun{}, err
			}
			snapshot.Runs[id] = run.Clone()
			return run.Clone(), nil
		})
}

func (s *Service) CompleteRun(
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
			formula, ok := snapshot.Formulas[run.FormulaID]
			if !ok {
				return RunVesselPayload{}, domain.NewError(domain.CodeFormulaNotFound,
					"attached formula was not found")
			}
			if err := run.Complete(formula.TargetOriginalGravity, s.clock.Now()); err != nil {
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
			if err := vessel.StartCleaning(run.ID, s.clock.Now()); err != nil {
				return RunVesselPayload{}, err
			}
			snapshot.Runs[id] = run.Clone()
			snapshot.Vessels[vessel.ID] = vessel
			return RunVesselPayload{Run: run.Clone(), Vessel: vessel}, nil
		})
}
