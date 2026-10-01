package httpapi

import (
	"net/http"

	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/domain"
	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/service"
)

type createFormulaRequest struct {
	Name                  string  `json:"name"`
	Style                 string  `json:"style"`
	TargetOriginalGravity float64 `json:"target_original_gravity"`
	TargetFinalGravity    float64 `json:"target_final_gravity"`
	MaxFermentationTempC  float64 `json:"max_fermentation_temp_c"`
	MinimumDays           int     `json:"minimum_days"`
	MaximumDays           int     `json:"maximum_days"`
}

type setStepsRequest struct {
	Steps []domain.FormulaStep `json:"steps"`
}

func registerFormulaRoutes(mux *http.ServeMux, app *service.Service) {
	mux.HandleFunc("POST /v1/formulas", func(response http.ResponseWriter, request *http.Request) {
		body, err := decodeJSON[createFormulaRequest](response, request)
		if !handleDecode(response, err) {
			return
		}
		cmd, err := parseCommand(request, "formula.create", "", body, nil)
		if !handleDecode(response, err) {
			return
		}
		result, err := app.CreateFormula(service.CreateFormulaInput{
			Name:                  body.Name,
			Style:                 body.Style,
			TargetOriginalGravity: body.TargetOriginalGravity,
			TargetFinalGravity:    body.TargetFinalGravity,
			MaxFermentationTempC:  body.MaxFermentationTempC,
			MinimumDays:           body.MinimumDays,
			MaximumDays:           body.MaximumDays,
			Command:               cmd,
		})
		if err != nil {
			writeDomainError(response, err)
			return
		}
		writeCommandResult(response, result)
	})

	mux.HandleFunc("GET /v1/formulas", func(response http.ResponseWriter, _ *http.Request) {
		formulas, err := app.ListFormulas()
		if err != nil {
			writeDomainError(response, err)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"formulas": formulas})
	})

	mux.HandleFunc("GET /v1/formulas/{id}", func(response http.ResponseWriter, request *http.Request) {
		formula, err := app.GetFormula(request.PathValue("id"))
		if err != nil {
			writeDomainError(response, err)
			return
		}
		writeJSON(response, http.StatusOK, formula)
	})

	mux.HandleFunc("PUT /v1/formulas/{id}/steps", func(response http.ResponseWriter, request *http.Request) {
		body, err := decodeJSON[setStepsRequest](response, request)
		if !handleDecode(response, err) {
			return
		}
		expected, err := expectedVersion(request)
		if !handleDecode(response, err) {
			return
		}
		target := request.PathValue("id")
		cmd, err := parseCommand(request, "formula.set_steps", target, body, expected)
		if !handleDecode(response, err) {
			return
		}
		result, err := app.SetFormulaSteps(target,
			service.SetFormulaStepsInput{
				ExpectedVersion: expected,
				Steps:           body.Steps,
				Command:         cmd,
			})
		if err != nil {
			writeDomainError(response, err)
			return
		}
		writeCommandResult(response, result)
	})

	mux.HandleFunc("POST /v1/formulas/{id}/approve", func(response http.ResponseWriter, request *http.Request) {
		expected, err := expectedVersion(request)
		if !handleDecode(response, err) {
			return
		}
		target := request.PathValue("id")
		cmd, err := parseCommand(request, "formula.approve", target, struct{}{}, expected)
		if !handleDecode(response, err) {
			return
		}
		result, err := app.ApproveFormula(target,
			service.VersionInput{ExpectedVersion: expected, Command: cmd})
		if err != nil {
			writeDomainError(response, err)
			return
		}
		writeCommandResult(response, result)
	})

	mux.HandleFunc("POST /v1/formulas/{id}/retire", func(response http.ResponseWriter, request *http.Request) {
		expected, err := expectedVersion(request)
		if !handleDecode(response, err) {
			return
		}
		target := request.PathValue("id")
		cmd, err := parseCommand(request, "formula.retire", target, struct{}{}, expected)
		if !handleDecode(response, err) {
			return
		}
		result, err := app.RetireFormula(target,
			service.VersionInput{ExpectedVersion: expected, Command: cmd})
		if err != nil {
			writeDomainError(response, err)
			return
		}
		writeCommandResult(response, result)
	})
}
