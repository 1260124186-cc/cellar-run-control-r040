package httpapi

import (
	"net/http"
	"time"

	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/service"
)

type createRunRequest struct {
	Code           string  `json:"code"`
	FormulaID      string  `json:"formula_id"`
	PlannedVolumeL float64 `json:"planned_volume_liters"`
	Notes          string  `json:"notes"`
}

type reserveRunRequest struct {
	VesselID string `json:"vessel_id"`
}

type observationRequest struct {
	Sequence     int       `json:"sequence"`
	Gravity      float64   `json:"gravity"`
	TemperatureC float64   `json:"temperature_c"`
	ObservedAt   time.Time `json:"observed_at"`
	Note         string    `json:"note"`
}

func registerRunRoutes(mux *http.ServeMux, app *service.Service) {
	mux.HandleFunc("POST /v1/runs", func(response http.ResponseWriter, request *http.Request) {
		body, err := decodeJSON[createRunRequest](response, request)
		if !handleDecode(response, err) {
			return
		}
		instruction, err := buildInstruction(request, "POST /v1/runs", body)
		if !handleDecode(response, err) {
			return
		}
		result, err := app.CreateRun(service.CreateRunInput{
			Code:           body.Code,
			FormulaID:      body.FormulaID,
			PlannedVolumeL: body.PlannedVolumeL,
			Notes:          body.Notes,
			Instruction:    instruction,
		})
		if err != nil {
			writeDomainError(response, err)
			return
		}
		writeInstructionResult(response, result)
	})

	mux.HandleFunc("GET /v1/runs", func(response http.ResponseWriter, _ *http.Request) {
		runs, err := app.ListRuns()
		if err != nil {
			writeDomainError(response, err)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"runs": runs})
	})

	mux.HandleFunc("GET /v1/runs/{id}", func(response http.ResponseWriter, request *http.Request) {
		run, err := app.GetRun(request.PathValue("id"))
		if err != nil {
			writeDomainError(response, err)
			return
		}
		writeJSON(response, http.StatusOK, run)
	})

	mux.HandleFunc("POST /v1/runs/{id}/reserve", func(response http.ResponseWriter, request *http.Request) {
		body, err := decodeJSON[reserveRunRequest](response, request)
		if !handleDecode(response, err) {
			return
		}
		expected, err := expectedVersion(request)
		if !handleDecode(response, err) {
			return
		}
		route := "POST /v1/runs/{id}/reserve"
		instruction, err := buildInstruction(request, route, body)
		if !handleDecode(response, err) {
			return
		}
		result, err := app.ReserveRun(request.PathValue("id"), service.ReserveRunInput{
			ExpectedVersion: expected,
			VesselID:        body.VesselID,
			Instruction:     instruction,
		})
		if err != nil {
			writeDomainError(response, err)
			return
		}
		writeInstructionResult(response, result)
	})

	mux.HandleFunc("POST /v1/runs/{id}/start", func(response http.ResponseWriter, request *http.Request) {
		expected, err := expectedVersion(request)
		if !handleDecode(response, err) {
			return
		}
		route := "POST /v1/runs/{id}/start"
		instruction, err := buildInstruction(request, route, nil)
		if !handleDecode(response, err) {
			return
		}
		result, err := app.StartRun(request.PathValue("id"),
			service.VersionInput{ExpectedVersion: expected, Instruction: instruction})
		if err != nil {
			writeDomainError(response, err)
			return
		}
		writeInstructionResult(response, result)
	})

	mux.HandleFunc("POST /v1/runs/{id}/observations", func(response http.ResponseWriter, request *http.Request) {
		body, err := decodeJSON[observationRequest](response, request)
		if !handleDecode(response, err) {
			return
		}
		route := "POST /v1/runs/{id}/observations"
		instruction, err := buildInstruction(request, route, body)
		if !handleDecode(response, err) {
			return
		}
		result, err := app.AppendObservation(request.PathValue("id"), service.ObservationInput{
			Sequence:     body.Sequence,
			Gravity:      body.Gravity,
			TemperatureC: body.TemperatureC,
			ObservedAt:   body.ObservedAt,
			Note:         body.Note,
			Instruction:  instruction,
		})
		if err != nil {
			writeDomainError(response, err)
			return
		}
		writeInstructionResult(response, result)
	})

	mux.HandleFunc("POST /v1/runs/{id}/conditioning", func(response http.ResponseWriter, request *http.Request) {
		expected, err := expectedVersion(request)
		if !handleDecode(response, err) {
			return
		}
		route := "POST /v1/runs/{id}/conditioning"
		instruction, err := buildInstruction(request, route, nil)
		if !handleDecode(response, err) {
			return
		}
		result, err := app.BeginConditioning(request.PathValue("id"),
			service.VersionInput{ExpectedVersion: expected, Instruction: instruction})
		if err != nil {
			writeDomainError(response, err)
			return
		}
		writeInstructionResult(response, result)
	})

	mux.HandleFunc("POST /v1/runs/{id}/complete", func(response http.ResponseWriter, request *http.Request) {
		expected, err := expectedVersion(request)
		if !handleDecode(response, err) {
			return
		}
		route := "POST /v1/runs/{id}/complete"
		instruction, err := buildInstruction(request, route, nil)
		if !handleDecode(response, err) {
			return
		}
		result, err := app.CompleteRun(request.PathValue("id"),
			service.VersionInput{ExpectedVersion: expected, Instruction: instruction})
		if err != nil {
			writeDomainError(response, err)
			return
		}
		writeInstructionResult(response, result)
	})

	mux.HandleFunc("POST /v1/runs/{id}/abort", func(response http.ResponseWriter, request *http.Request) {
		expected, err := expectedVersion(request)
		if !handleDecode(response, err) {
			return
		}
		route := "POST /v1/runs/{id}/abort"
		instruction, err := buildInstruction(request, route, nil)
		if !handleDecode(response, err) {
			return
		}
		result, err := app.AbortRun(request.PathValue("id"),
			service.VersionInput{ExpectedVersion: expected, Instruction: instruction})
		if err != nil {
			writeDomainError(response, err)
			return
		}
		writeInstructionResult(response, result)
	})
}
