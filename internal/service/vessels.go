package service

import (
	"sort"

	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/domain"
)

func (s *Service) CreateVessel(input CreateVesselInput) (CommandResult, error) {
	now := s.clock.Now()
	vessel := domain.Vessel{
		Code:      domain.NormalizeVesselCode(input.Code),
		CapacityL: input.CapacityL,
		State:     domain.VesselAvailable,
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := vessel.Validate(); err != nil {
		return CommandResult{}, err
	}

	return s.runCommand(input.Command, statusCreated, func(snapshot *domain.Snapshot) (any, error) {
		id, err := generateEntityID("vessel")
		if err != nil {
			return nil, domain.NewError(domain.CodeStorage, err.Error())
		}
		vessel.ID = id
		if _, exists := snapshot.VesselIndex[vessel.Code]; exists {
			return nil, domain.NewError(domain.CodeVesselConflict,
				"a vessel with the same code already exists")
		}
		snapshot.Vessels[vessel.ID] = vessel
		snapshot.VesselIndex[vessel.Code] = vessel.ID
		return vessel, nil
	})
}

func (s *Service) GetVessel(id string) (domain.Vessel, error) {
	var found domain.Vessel
	err := s.store.View(func(snapshot domain.Snapshot) error {
		vessel, ok := snapshot.Vessels[id]
		if !ok {
			return domain.NewError(domain.CodeVesselNotFound, "vessel was not found")
		}
		found = vessel
		return nil
	})
	if err != nil {
		return domain.Vessel{}, err
	}
	return found, nil
}

func (s *Service) ListVessels() ([]domain.Vessel, error) {
	var vessels []domain.Vessel
	err := s.store.View(func(snapshot domain.Snapshot) error {
		vessels = make([]domain.Vessel, 0, len(snapshot.Vessels))
		for _, vessel := range snapshot.Vessels {
			vessels = append(vessels, vessel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(vessels, func(i, j int) bool {
		if vessels[i].Code == vessels[j].Code {
			return vessels[i].ID < vessels[j].ID
		}
		return vessels[i].Code < vessels[j].Code
	})
	return vessels, nil
}

func (s *Service) FinishCleaning(
	id string,
	input FinishCleaningInput,
) (CommandResult, error) {
	return s.runCommand(input.Command, statusOK, func(snapshot *domain.Snapshot) (any, error) {
		vessel, ok := snapshot.Vessels[id]
		if !ok {
			return nil, domain.NewError(domain.CodeVesselNotFound, "vessel was not found")
		}
		if err := s.checkExpected(vessel.Version, input.ExpectedVersion); err != nil {
			return nil, err
		}
		if vessel.ActiveRunID == nil {
			return nil, domain.NewError(domain.CodeVesselState,
				"cleaning vessel has no associated run")
		}
		run, ok := snapshot.Runs[*vessel.ActiveRunID]
		if !ok {
			return nil, domain.NewError(domain.CodeRunNotFound,
				"associated run was not found")
		}
		if run.State != domain.RunCompleted && run.State != domain.RunAborted {
			return nil, domain.NewError(domain.CodeRunState,
				"associated run is still open")
		}
		if err := vessel.FinishCleaning(s.clock.Now()); err != nil {
			return nil, err
		}
		snapshot.Vessels[id] = vessel
		return vessel, nil
	})
}

func (s *Service) RetireVessel(id string, input VersionInput) (CommandResult, error) {
	return s.runCommand(input.Command, statusOK, func(snapshot *domain.Snapshot) (any, error) {
		vessel, ok := snapshot.Vessels[id]
		if !ok {
			return nil, domain.NewError(domain.CodeVesselNotFound, "vessel was not found")
		}
		if err := s.checkExpected(vessel.Version, input.ExpectedVersion); err != nil {
			return nil, err
		}
		if err := vessel.Retire(s.clock.Now()); err != nil {
			return nil, err
		}
		snapshot.Vessels[id] = vessel
		return vessel, nil
	})
}
