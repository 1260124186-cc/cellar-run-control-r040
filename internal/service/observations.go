package service

import "github.com/1260124186-cc/solo-0016-cellar-run-control/internal/domain"

func (s *Service) AppendObservation(
	id string,
	input ObservationInput,
) (CommandResult, error) {
	return s.runCommand(input.Command, statusOK, func(snapshot *domain.Snapshot) (any, error) {
		run, ok := snapshot.Runs[id]
		if !ok {
			return nil, domain.NewError(domain.CodeRunNotFound, "run was not found")
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
			return nil, err
		}
		snapshot.Runs[id] = run.Clone()
		return run.Clone(), nil
	})
}

func (s *Service) BeginConditioning(
	id string,
	input VersionInput,
) (CommandResult, error) {
	return s.runCommand(input.Command, statusOK, func(snapshot *domain.Snapshot) (any, error) {
		run, ok := snapshot.Runs[id]
		if !ok {
			return nil, domain.NewError(domain.CodeRunNotFound, "run was not found")
		}
		if err := s.checkExpected(run.Version, input.ExpectedVersion); err != nil {
			return nil, err
		}
		if err := run.BeginConditioning(s.clock.Now()); err != nil {
			return nil, err
		}
		snapshot.Runs[id] = run.Clone()
		return run.Clone(), nil
	})
}

func (s *Service) CompleteRun(
	id string,
	input VersionInput,
) (CommandResult, error) {
	return s.runCommand(input.Command, statusOK, func(snapshot *domain.Snapshot) (any, error) {
		run, ok := snapshot.Runs[id]
		if !ok {
			return nil, domain.NewError(domain.CodeRunNotFound, "run was not found")
		}
		if err := s.checkExpected(run.Version, input.ExpectedVersion); err != nil {
			return nil, err
		}
		formula, ok := snapshot.Formulas[run.FormulaID]
		if !ok {
			return nil, domain.NewError(domain.CodeFormulaNotFound, "attached formula was not found")
		}
		if err := run.Complete(formula.TargetOriginalGravity, s.clock.Now()); err != nil {
			return nil, err
		}
		if run.VesselID == nil {
			return nil, domain.NewError(domain.CodeRunState, "run has no vessel")
		}
		vessel, ok := snapshot.Vessels[*run.VesselID]
		if !ok {
			return nil, domain.NewError(domain.CodeVesselNotFound, "vessel was not found")
		}
		if err := vessel.StartCleaning(run.ID, s.clock.Now()); err != nil {
			return nil, err
		}
		snapshot.Runs[id] = run.Clone()
		snapshot.Vessels[vessel.ID] = vessel
		return runVesselPair{Run: run.Clone(), Vessel: vessel}, nil
	})
}
