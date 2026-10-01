package workflowcheck

import (
	"fmt"
	"net/http/httptest"
	"os"

	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/clock"
	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/httpapi"
	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/service"
	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/storage"
)

// Run executes a named production workflow check. Most checks use the shared
// temporary server; the restart check manages its own data directories so it
// can reopen a persisted snapshot between "processes".
func Run(name string) error {
	var check func(*client) error
	managesOwnServer := false
	switch name {
	case "formula-approval":
		check = formulaApproval
	case "vessel-run-lifecycle":
		check = vesselRunLifecycle
	case "observations-completion":
		check = observationsCompletion
	case "command-receipts":
		check = commandReceipts
	case "command-receipts-restart":
		managesOwnServer = true
	default:
		return fmt.Errorf("unknown workflow check %q", name)
	}

	if managesOwnServer {
		if err := commandReceiptsRestart(); err != nil {
			return err
		}
	} else {
		dataDir, err := os.MkdirTemp("", "cellar-run-check-*")
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
		err = check(api)
		server.Close()
		if err != nil {
			return err
		}
	}
	fmt.Printf("workflow check passed: %s\n", name)
	return nil
}
