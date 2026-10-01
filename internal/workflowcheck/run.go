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

func Run(name string) error {
	switch name {
	case "instruction-receipts":
		// This check owns a restart cycle, so it manages its own fixtures.
		if err := instructionReceipts(); err != nil {
			return err
		}
	default:
		if err := runWithServer(name); err != nil {
			return err
		}
	}
	fmt.Printf("workflow check passed: %s\n", name)
	return nil
}

func runWithServer(name string) error {
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
	defer server.Close()
	api := newClient(server.URL)

	switch name {
	case "formula-approval":
		return formulaApproval(api)
	case "vessel-run-lifecycle":
		return vesselRunLifecycle(api)
	case "observations-completion":
		return observationsCompletion(api)
	default:
		return fmt.Errorf("unknown workflow check %q", name)
	}
}
