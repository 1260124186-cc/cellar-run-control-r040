package service

import (
	"time"

	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/domain"
)

type CreateFormulaInput struct {
	Name                  string
	Style                 string
	TargetOriginalGravity float64
	TargetFinalGravity    float64
	MaxFermentationTempC  float64
	MinimumDays           int
	MaximumDays           int
	Command               *Command
}

type SetFormulaStepsInput struct {
	ExpectedVersion *int64
	Steps           []domain.FormulaStep
	Command         *Command
}

type VersionInput struct {
	ExpectedVersion *int64
	Command         *Command
}

type CreateVesselInput struct {
	Code      string
	CapacityL float64
	Command   *Command
}

type CreateRunInput struct {
	Code           string
	FormulaID      string
	PlannedVolumeL float64
	Notes          string
	Command        *Command
}

type ReserveRunInput struct {
	ExpectedVersion *int64
	VesselID        string
	Command         *Command
}

type ObservationInput struct {
	Sequence     int
	Gravity      float64
	TemperatureC float64
	ObservedAt   time.Time
	Note         string
	Command      *Command
}

type FinishCleaningInput struct {
	ExpectedVersion *int64
	Command         *Command
}
