package workflowcheck

import (
	"bytes"
	"fmt"
	"net/http/httptest"
	"os"
	"sync"
	"time"

	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/clock"
	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/domain"
	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/httpapi"
	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/service"
	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/storage"
)

// instructionReceipts verifies the independent instruction receipt layer:
// retries collapse into one business fact and replay the first response, a
// key reused with other parameters is reported occupied, failed attempts do
// not reserve the key, concurrent duplicate submissions deduplicate, and the
// outcome survives a service restart.
func instructionReceipts() error {
	dataDir, err := os.MkdirTemp("", "cellar-receipt-check-*")
	if err != nil {
		return fmt.Errorf("create check directory: %w", err)
	}
	defer os.RemoveAll(dataDir)

	store, err := storage.Open(dataDir)
	if err != nil {
		return err
	}
	app := service.New(store, clock.System{})
	server := httptest.NewServer(httpapi.NewHandler(app))
	api := newClient(server.URL)

	formula, err := ensureApprovedFormula(api, "Receipt Formula")
	if err != nil {
		return err
	}
	vessel, err := createCheckVessel(api, "VSL-777", 500)
	if err != nil {
		return err
	}

	runPayload := createRunPayload("RUN-RECEIPT", formula.ID)
	createKey := "TK-RUN-CREATE-0001"
	first, err := api.callWith("POST", "/v1/runs", runPayload, requestOptions{
		instructionKey: createKey,
		terminalID:     "terminal-A",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(first, 201, "create run with instruction key"); err != nil {
		return err
	}
	if first.replay {
		return errInvalidWorkflow("first instruction was marked as a replay")
	}
	var created runView
	if err := first.decode(&created); err != nil {
		return err
	}

	// A resubmission after a timeout returns the first response verbatim and
	// does not create a second run or advance any state.
	retry, err := api.callWith("POST", "/v1/runs", runPayload, requestOptions{
		instructionKey: createKey,
		terminalID:     "terminal-A",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(retry, 201, "retry create with same instruction key"); err != nil {
		return err
	}
	if !retry.replay {
		return errInvalidWorkflow("retry was not marked as a replay")
	}
	if !bytes.Equal(first.body, retry.body) {
		return errInvalidWorkflow("replayed response body differs from the first response")
	}
	if err := requireSingleRun(api, "RUN-RECEIPT"); err != nil {
		return err
	}

	// Reusing the key with a different set of parameters must explicitly say
	// the instruction key is already occupied, without changing anything.
	other := createRunPayload("RUN-RECEIPT", formula.ID)
	other["planned_volume_liters"] = 220.0
	occupied, err := api.callWith("POST", "/v1/runs", other, requestOptions{
		instructionKey: createKey,
		terminalID:     "terminal-A",
	})
	if err != nil {
		return err
	}
	if err := requireCode(occupied, 409, string(domain.CodeInstructionOccupied),
		"reuse instruction key with different parameters"); err != nil {
		return err
	}
	if err := requireSingleRun(api, "RUN-RECEIPT"); err != nil {
		return err
	}

	// A failed instruction attempt must not lock the key: the same key later
	// succeeds once the request is valid.
	reserveKey := "TK-RESERVE-0002"
	failed, err := api.callWith("POST", "/v1/runs/"+created.ID+"/reserve",
		map[string]any{"vessel_id": "vessel_does_not_exist"},
		requestOptions{instructionKey: reserveKey})
	if err != nil {
		return err
	}
	if err := requireCode(failed, 404, string(domain.CodeVesselNotFound),
		"fail instruction attempt"); err != nil {
		return err
	}
	success, err := api.callWith("POST", "/v1/runs/"+created.ID+"/reserve",
		map[string]any{"vessel_id": vessel.ID},
		requestOptions{instructionKey: reserveKey})
	if err != nil {
		return err
	}
	if err := requireStatus(success, 200, "retry previously failed key with valid parameters"); err != nil {
		return err
	}
	if success.replay {
		return errInvalidWorkflow("recovered instruction was treated as a replay")
	}
	var reserved pairView
	if err := success.decode(&reserved); err != nil {
		return err
	}

	// State advancement replays too: duplicate start requests yield one
	// fermenting run and the same response.
	startKey := "TK-START-0003"
	start1, err := api.callWith("POST", "/v1/runs/"+created.ID+"/start", nil,
		requestOptions{instructionKey: startKey, expectedVersion: ptrInt64(reserved.Run.Version)})
	if err != nil {
		return err
	}
	if err := requireStatus(start1, 200, "start run with key"); err != nil {
		return err
	}
	start2, err := api.callWith("POST", "/v1/runs/"+created.ID+"/start", nil,
		requestOptions{instructionKey: startKey, expectedVersion: ptrInt64(reserved.Run.Version)})
	if err != nil {
		return err
	}
	if err := requireStatus(start2, 200, "duplicate start with same key"); err != nil {
		return err
	}
	if !start2.replay || !bytes.Equal(start1.body, start2.body) {
		return errInvalidWorkflow("duplicate start did not replay the first response")
	}
	var started pairView
	if err := start1.decode(&started); err != nil {
		return err
	}
	if started.Run.State != domain.RunFermenting {
		return errInvalidWorkflow("run did not enter fermenting exactly once")
	}

	// Two terminals firing the same key with the same parameters at once:
	// exactly one wins and the other receives the stored replay.
	obsPayload := map[string]any{
		"sequence":      1,
		"gravity":       1.05,
		"temperature_c": 20,
		"observed_at":   time.Now().UTC().Add(10 * time.Second).Format(time.RFC3339),
		"note":          "race observation",
	}
	obsKey := "TK-OBS-RACE-0004"
	var barrier sync.WaitGroup
	results := make(chan response, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		barrier.Add(1)
		go func(terminal int) {
			defer barrier.Done()
			resp, callErr := api.callWith("POST", "/v1/runs/"+created.ID+"/observations",
				obsPayload,
				requestOptions{
					instructionKey: obsKey,
					terminalID:     fmt.Sprintf("terminal-%d", terminal),
				})
			if callErr != nil {
				errs <- callErr
				return
			}
			results <- resp
		}(i)
	}
	barrier.Wait()
	close(results)
	close(errs)
	for callErr := range errs {
		return callErr
	}
	var accepted, replayed int
	for resp := range results {
		if resp.status != 200 {
			return fmt.Errorf("racing observation returned status %d: %s",
				resp.status, resp.body)
		}
		if resp.replay {
			replayed++
		} else {
			accepted++
		}
	}
	if accepted != 1 || replayed != 1 {
		return errInvalidWorkflow("racing duplicates were not deduplicated to one fact")
	}

	// After a full service restart the stored receipt still resolves to the
	// first response and the business facts remain single.
	server.Close()
	reopenedStore, err := storage.Open(dataDir)
	if err != nil {
		return err
	}
	reopenedApp := service.New(reopenedStore, clock.System{})
	restartedServer := httptest.NewServer(httpapi.NewHandler(reopenedApp))
	defer restartedServer.Close()
	restartAPI := newClient(restartedServer.URL)
	afterRestart, err := restartAPI.callWith("POST", "/v1/runs", runPayload,
		requestOptions{instructionKey: createKey, terminalID: "terminal-A"})
	if err != nil {
		return err
	}
	if err := requireStatus(afterRestart, 201, "replay after restart"); err != nil {
		return err
	}
	if !afterRestart.replay || !bytes.Equal(first.body, afterRestart.body) {
		return errInvalidWorkflow("receipt did not survive the restart unchanged")
	}
	readRun, err := restartAPI.call("GET", "/v1/runs/"+created.ID, nil, nil)
	if err != nil {
		return err
	}
	var restartedRun runView
	if err := readRun.decode(&restartedRun); err != nil {
		return err
	}
	if restartedRun.State != domain.RunFermenting || len(restartedRun.Observations) != 1 {
		return errInvalidWorkflow("restart exposed more than one business fact")
	}
	return nil
}

func requireSingleRun(api *client, code string) error {
	listResponse, err := api.call("GET", "/v1/runs", nil, nil)
	if err != nil {
		return err
	}
	if err := requireStatus(listResponse, 200, "list runs"); err != nil {
		return err
	}
	var runList struct {
		Runs []runView `json:"runs"`
	}
	if err := listResponse.decode(&runList); err != nil {
		return err
	}
	count := 0
	for _, run := range runList.Runs {
		if run.Code == code {
			count++
		}
	}
	if count != 1 {
		return errInvalidWorkflow(fmt.Sprintf(
			"expected exactly one run with code %s, found %d", code, count))
	}
	return nil
}
