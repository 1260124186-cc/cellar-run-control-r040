package httpapi

import (
	"net/http"

	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/service"
)

type createVesselRequest struct {
	Code      string  `json:"code"`
	CapacityL float64 `json:"capacity_liters"`
}

func registerVesselRoutes(mux *http.ServeMux, app *service.Service) {
	mux.HandleFunc("POST /v1/vessels", func(response http.ResponseWriter, request *http.Request) {
		body, err := decodeJSON[createVesselRequest](response, request)
		if !handleDecode(response, err) {
			return
		}
		cmd, err := parseCommand(request, "vessel.create", "", body, nil)
		if !handleDecode(response, err) {
			return
		}
		result, err := app.CreateVessel(service.CreateVesselInput{
			Code:      body.Code,
			CapacityL: body.CapacityL,
			Command:   cmd,
		})
		if err != nil {
			writeDomainError(response, err)
			return
		}
		writeCommandResult(response, result)
	})

	mux.HandleFunc("GET /v1/vessels", func(response http.ResponseWriter, _ *http.Request) {
		vessels, err := app.ListVessels()
		if err != nil {
			writeDomainError(response, err)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"vessels": vessels})
	})

	mux.HandleFunc("GET /v1/vessels/{id}", func(response http.ResponseWriter, request *http.Request) {
		vessel, err := app.GetVessel(request.PathValue("id"))
		if err != nil {
			writeDomainError(response, err)
			return
		}
		writeJSON(response, http.StatusOK, vessel)
	})

	mux.HandleFunc("POST /v1/vessels/{id}/finish-cleaning", func(response http.ResponseWriter, request *http.Request) {
		expected, err := expectedVersion(request)
		if !handleDecode(response, err) {
			return
		}
		target := request.PathValue("id")
		cmd, err := parseCommand(request, "vessel.finish_cleaning", target, struct{}{}, expected)
		if !handleDecode(response, err) {
			return
		}
		result, err := app.FinishCleaning(target,
			service.FinishCleaningInput{ExpectedVersion: expected, Command: cmd})
		if err != nil {
			writeDomainError(response, err)
			return
		}
		writeCommandResult(response, result)
	})

	mux.HandleFunc("POST /v1/vessels/{id}/retire", func(response http.ResponseWriter, request *http.Request) {
		expected, err := expectedVersion(request)
		if !handleDecode(response, err) {
			return
		}
		target := request.PathValue("id")
		cmd, err := parseCommand(request, "vessel.retire", target, struct{}{}, expected)
		if !handleDecode(response, err) {
			return
		}
		result, err := app.RetireVessel(target,
			service.VersionInput{ExpectedVersion: expected, Command: cmd})
		if err != nil {
			writeDomainError(response, err)
			return
		}
		writeCommandResult(response, result)
	})
}
