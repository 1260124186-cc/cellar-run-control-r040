package workflowcheck

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/domain"
)

// commandReceipts verifies the independent command receipt mechanism:
//
//   - a repeated create/advance command collapses into one business fact and
//     replays the first response byte-for-byte;
//   - reusing a command number with changed parameters is rejected as an
//     occupied command number;
//   - two terminals racing for the same command number get one execution plus
//     one replay, never two facts;
//   - a failed attempt does not lock the command number;
//   - restarting the service with the same snapshot keeps replays working.
func commandReceipts(api *client) error {
	formula, err := ensureApprovedFormula(api, "Receipt Formula")
	if err != nil {
		return err
	}
	vessel, err := createCheckVessel(api, "VSL-303", 500)
	if err != nil {
		return err
	}

	terminal := "terminal-floor-7"
	workOrder := "WO-5501"
	createPayload := func(code string) map[string]any {
		payload := createRunPayload(code, formula.ID)
		return payload
	}

	// 1. Create a run under a command number, then resend the identical
	// request as if the first response had timed out on weak signal.
	createCmd := &command{terminalID: terminal, number: "CMD-1001", workOrder: workOrder}
	first, err := api.callWithCommand("POST", "/v1/runs",
		createPayload("RUN-RECEIPT-A"), nil, createCmd)
	if err != nil {
		return err
	}
	if err := requireStatus(first, 201, "create run with command"); err != nil {
		return err
	}
	if first.replay {
		return errInvalidWorkflow("first command execution was flagged as a replay")
	}
	var firstRun runView
	if err := first.decode(&firstRun); err != nil {
		return err
	}

	retry, err := api.callWithCommand("POST", "/v1/runs",
		createPayload("RUN-RECEIPT-A"), nil, createCmd)
	if err != nil {
		return err
	}
	if err := requireStatus(retry, 201, "resend identical create command"); err != nil {
		return err
	}
	if !retry.replay {
		return errInvalidWorkflow("resent command did not return an idempotent replay marker")
	}
	if string(retry.body) != string(first.body) {
		return errInvalidWorkflow("replayed response body differs from the first response")
	}
	var retriedRun runView
	if err := retry.decode(&retriedRun); err != nil {
		return err
	}
	if retriedRun.ID != firstRun.ID {
		return errInvalidWorkflow("retry created a second run instead of replaying the first")
	}

	// 2. Same command number with changed parameters must be rejected as an
	// occupied number, and must not create anything.
	tampered, err := api.callWithCommand("POST", "/v1/runs",
		createPayload("RUN-RECEIPT-DIFFERENT"), nil,
		&command{terminalID: terminal, number: "CMD-1001", workOrder: workOrder})
	if err != nil {
		return err
	}
	if err := requireCode(tampered, http.StatusConflict,
		string(domain.CodeCommandReuseConflict),
		"reuse command number with different parameters"); err != nil {
		return err
	}

	// A different work order on the same number is also a reuse conflict.
	otherOrder, err := api.callWithCommand("POST", "/v1/runs",
		createPayload("RUN-RECEIPT-A"), nil,
		&command{terminalID: terminal, number: "CMD-1001", workOrder: "WO-9999"})
	if err != nil {
		return err
	}
	if err := requireCode(otherOrder, http.StatusConflict,
		string(domain.CodeCommandReuseConflict),
		"reuse command number under another work order"); err != nil {
		return err
	}

	// 3. Lifecycle advances are idempotent too: reserve and start once,
	// resend each command, and assert the aggregate versions never move
	// twice.
	reserveCmd := &command{terminalID: terminal, number: "CMD-1002", workOrder: workOrder}
	reserveBody := map[string]any{"vessel_id": vessel.ID}
	reserveFirst, err := api.callWithCommand("POST", "/v1/runs/"+firstRun.ID+"/reserve",
		reserveBody, ptrInt64(firstRun.Version), reserveCmd)
	if err != nil {
		return err
	}
	if err := requireStatus(reserveFirst, 200, "reserve with command"); err != nil {
		return err
	}
	var reserved pairView
	if err := reserveFirst.decode(&reserved); err != nil {
		return err
	}
	reserveRetry, err := api.callWithCommand("POST", "/v1/runs/"+firstRun.ID+"/reserve",
		reserveBody, ptrInt64(firstRun.Version), reserveCmd)
	if err != nil {
		return err
	}
	if err := requireStatus(reserveRetry, 200, "resend reserve command"); err != nil {
		return err
	}
	if !reserveRetry.replay || string(reserveRetry.body) != string(reserveFirst.body) {
		return errInvalidWorkflow("reserve replay did not return the original response")
	}
	var reservedAgain pairView
	if err := reserveRetry.decode(&reservedAgain); err != nil {
		return err
	}
	if reservedAgain.Run.Version != reserved.Run.Version {
		return errInvalidWorkflow("reserve replay advanced the run version a second time")
	}

	startCmd := &command{terminalID: terminal, number: "CMD-1003", workOrder: workOrder}
	startFirst, err := api.callWithCommand("POST", "/v1/runs/"+firstRun.ID+"/start",
		nil, ptrInt64(reserved.Run.Version), startCmd)
	if err != nil {
		return err
	}
	if err := requireStatus(startFirst, 200, "start with command"); err != nil {
		return err
	}
	var started pairView
	if err := startFirst.decode(&started); err != nil {
		return err
	}
	if err := requireRunState(started.Run, domain.RunFermenting); err != nil {
		return err
	}
	startRetry, err := api.callWithCommand("POST", "/v1/runs/"+firstRun.ID+"/start",
		nil, ptrInt64(reserved.Run.Version), startCmd)
	if err != nil {
		return err
	}
	if !startRetry.replay {
		return errInvalidWorkflow("resent start command did not replay")
	}

	// 4. A command number used by a request that fails business validation is
	// never locked: the same number can then succeed.
	failingCmd := &command{terminalID: terminal, number: "CMD-1004", workOrder: workOrder}
	failed, err := api.callWithCommand("POST", "/v1/runs",
		createRunPayload("RUN-BADCODE", "missing-formula"), nil, failingCmd)
	if err != nil {
		return err
	}
	if err := requireCode(failed, http.StatusNotFound,
		string(domain.CodeFormulaNotFound),
		"first attempt with unknown formula fails"); err != nil {
		return err
	}
	recovered, err := api.callWithCommand("POST", "/v1/runs",
		createPayload("RUN-RECEIPT-B"), nil, failingCmd)
	if err != nil {
		return err
	}
	if err := requireStatus(recovered, 201, "reuse command number after failure"); err != nil {
		return err
	}
	if recovered.replay {
		return errInvalidWorkflow("a failed attempt must not produce a replay on retry")
	}

	// 5. Concurrent terminals presenting the same command number result in
	// exactly one run. Fire identical requests in parallel and count
	// executions versus replays.
	raceCmd := &command{terminalID: "terminal-race", number: "CMD-2001", workOrder: workOrder}
	racePayload := createPayload("RUN-RECEIPT-RACE")
	const racers = 8
	type raceOutcome struct {
		resp response
		err  error
	}
	outcomes := make([]raceOutcome, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			resp, callErr := api.callWithCommand("POST", "/v1/runs",
				racePayload, nil, raceCmd)
			outcomes[index] = raceOutcome{resp: resp, err: callErr}
		}(i)
	}
	wg.Wait()
	executions, replays := 0, 0
	var raceID string
	for _, outcome := range outcomes {
		if outcome.err != nil {
			return outcome.err
		}
		if err := requireStatus(outcome.resp, 201, "racing create command"); err != nil {
			return err
		}
		var view runView
		if err := outcome.resp.decode(&view); err != nil {
			return err
		}
		if raceID == "" {
			raceID = view.ID
		} else if view.ID != raceID {
			return errInvalidWorkflow("racing requests produced more than one run")
		}
		if outcome.resp.replay {
			replays++
		} else {
			executions++
		}
	}
	if executions != 1 || replays != racers-1 {
		return fmt.Errorf("race produced %d executions and %d replays, want 1 and %d",
			executions, replays, racers-1)
	}

	// 6. Different terminals have independent command number spaces.
	otherTerminal, err := api.callWithCommand("POST", "/v1/runs",
		createPayload("RUN-RECEIPT-C"), nil,
		&command{terminalID: "terminal-floor-9", number: "CMD-1001", workOrder: workOrder})
	if err != nil {
		return err
	}
	if err := requireStatus(otherTerminal, 201,
		"same number on a different terminal executes independently"); err != nil {
		return err
	}

	// 7. Only one header without the other is rejected as a malformed command.
	halfHeader, err := api.callWithCommand("POST", "/v1/runs",
		createPayload("RUN-RECEIPT-D"), nil,
		&command{terminalID: "", number: "CMD-7001"})
	if err != nil {
		return err
	}
	if err := requireCode(halfHeader, http.StatusBadRequest,
		string(domain.CodeCommandHeaderInvalid),
		"reject command number without terminal id"); err != nil {
		return err
	}

	// 8. Only one run with the race code must exist. Listing is a stable
	// read unaffected by any receipt machinery.
	listed, err := api.call("GET", "/v1/runs", nil, nil)
	if err != nil {
		return err
	}
	if err := requireStatus(listed, 200, "list runs after receipts"); err != nil {
		return err
	}
	var runList struct {
		Runs []runView `json:"runs"`
	}
	if err := listed.decode(&runList); err != nil {
		return err
	}
	codeCount := 0
	for _, run := range runList.Runs {
		if run.ID == raceID {
			codeCount++
		}
	}
	if codeCount != 1 {
		return fmt.Errorf("expected exactly one raced run, found %d", codeCount)
	}
	return nil
}
