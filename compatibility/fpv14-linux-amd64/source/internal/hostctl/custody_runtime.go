package hostctl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

// Called by the authenticated local policy controller, never by arbitrary Web
// command/path input. A configured source must prove its actual loaded key;
// absent source configuration is a legitimate waiting_for_source state.
func (e *Engine) ReviewBodyLoaded(ctx context.Context) error {
	if err := requireMutationAuthority(e.Paths); err != nil {
		return err
	}
	return e.withOperationLock(func() error {
		if err := e.ensureNoOutstandingOperation(); err != nil {
			return err
		}
		return e.proveReviewBodyLoaded(ctx)
	})
}

func (e *Engine) proveReviewBodyLoaded(ctx context.Context) error {
	return e.proveReviewBodyLoadedConsumers(ctx, true)
}
func (e *Engine) proveReviewBodyLoadedConsumers(ctx context.Context, includeSource bool) error {
	c, active, present, err := e.custodyBinding()
	if err != nil || !present {
		return fmt.Errorf("restore_original_review_body_key_required")
	}
	if !active {
		return fmt.Errorf("review body custody is disabled; enable the original binding with the managed tool")
	}
	if err = e.ValidateReviewBodyConfig(); err != nil {
		return err
	}
	keys, err := reviewbody.LoadKeyRingFromFile(c.KeyID, c.KeyFile)
	if err != nil {
		return err
	}
	check := func(unit string) error {
		state, err := e.systemdUnitProperty(ctx, unit, "ActiveState")
		if err != nil || state != "active" {
			return fmt.Errorf("body consumer is not active")
		}
		invocation, err := e.systemdUnitProperty(ctx, unit, "InvocationID")
		if err != nil {
			return err
		}
		pidRaw, err := e.systemdUnitProperty(ctx, unit, "MainPID")
		if err != nil {
			return err
		}
		pid, err := strconv.Atoi(pidRaw)
		if err != nil || pid < 1 {
			return fmt.Errorf("body consumer is not running")
		}
		var proof reviewbody.LoadedProof
		if unit == "xinsight.service" {
			raw, err := boundedGET(ctx, e.HTTPClient, "http://127.0.0.1:8080/readyz")
			if err != nil {
				return err
			}
			var result struct {
				Status string                  `json:"status"`
				Proof  *reviewbody.LoadedProof `json:"review_body_loaded,omitempty"`
			}
			if json.Unmarshal(raw, &result) != nil || result.Status != "ready" || result.Proof == nil {
				return fmt.Errorf("reader body capability pending")
			}
			proof = *result.Proof
		} else {
			path := reviewbody.WorkerLoadedProofPath
			if e.Paths.IsTestRoot {
				path = filepath.Join(e.Paths.Root, path)
			}
			if err = readSourceLoadedProof(path, &proof); err != nil {
				return err
			}
		}
		if proof.Unit != unit || proof.PID != pid || proof.InvocationID != invocation {
			return fmt.Errorf("body consumer proof is stale")
		}
		if err = keys.VerifyLoadedProof(proof); err != nil {
			return fmt.Errorf("body consumer loaded a different key")
		}
		after, err := e.systemdUnitProperty(ctx, unit, "InvocationID")
		if err != nil || after != invocation {
			return fmt.Errorf("body consumer restarted during readback")
		}
		return nil
	}
	if err = check("xinsight.service"); err != nil {
		return err
	}
	if !includeSource {
		return nil
	}
	if configured, err := e.hasCustodySourceConfiguration(); err != nil {
		return err
	} else if configured {
		return check(sourceFactsWorkerUnit)
	}
	return nil
}

func (e *Engine) hasCustodySourceConfiguration() (bool, error) {
	if present, err := fileExists(e.Paths.SourceFactsConfig); err != nil || present {
		return present, err
	}
	dir := sourcefacts.ManagedPartitionsDirectory(e.Paths.SourceFactsConfig)
	if present, err := fileExists(dir); err != nil || !present {
		return false, err
	}
	partitions, err := e.loadManagedProjectPartitions()
	return len(partitions) > 0, err
}

func readSourceLoadedProof(path string, proof *reviewbody.LoadedProof) error {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("source body capability pending")
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Nlink != 1 || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() < 1 || info.Size() > 4096 {
		return fmt.Errorf("source body capability pending")
	}
	d := json.NewDecoder(io.LimitReader(f, 4097))
	d.DisallowUnknownFields()
	if err = d.Decode(proof); err != nil {
		return fmt.Errorf("source body capability pending")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return fmt.Errorf("source body capability pending")
	}
	return nil
}
