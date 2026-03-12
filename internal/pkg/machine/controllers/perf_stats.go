// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/talos/pkg/machinery/resources/perf"
	"go.uber.org/zap"
)

const (
	perfUpdateInterval = 30 * time.Second
	// perfInitialEvents matches the tailEvents default in the Omni frontend chart component.
	perfInitialEvents = 25
)

// PerfStatsController periodically publishes fake CPUStats and MemoryStats.
type PerfStatsController struct{}

// Name implements controller.Controller interface.
func (ctrl *PerfStatsController) Name() string {
	return "perf.StatsController"
}

// Inputs implements controller.Controller interface.
func (ctrl *PerfStatsController) Inputs() []controller.Input {
	return nil
}

// Outputs implements controller.Controller interface.
func (ctrl *PerfStatsController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: perf.CPUType,
			Kind: controller.OutputExclusive,
		},
		{
			Type: perf.MemoryType,
			Kind: controller.OutputExclusive,
		},
	}
}

// Run implements controller.Controller interface.
func (ctrl *PerfStatsController) Run(ctx context.Context, r controller.Runtime, _ *zap.Logger) error {
	// Migrate: remove any legacy perf resources created with no owner (by old machine.go static init).
	// The controller needs to own them so it can issue updates.
	for _, md := range []resource.Pointer{perf.NewCPU().Metadata(), perf.NewMemory().Metadata()} {
		existing, err := r.Get(ctx, md)
		if err != nil {
			if state.IsNotFoundError(err) {
				continue
			}

			return err
		}

		if existing.Metadata().Owner() == "" {
			if err = r.Destroy(ctx, md, controller.WithOwner("")); err != nil {
				return err
			}
		}
	}

	// Pre-populate history so watchers using tailEvents (e.g. Omni chart default of 25) get a full backlog.
	for range perfInitialEvents {
		if err := ctrl.reconcile(ctx, r); err != nil {
			return err
		}
	}

	ticker := time.NewTicker(perfUpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}

		if err := ctrl.reconcile(ctx, r); err != nil {
			return err
		}
	}
}

func (ctrl *PerfStatsController) reconcile(ctx context.Context, r controller.Runtime) error {
	// CPUStat fields are cumulative jiffies (USER_HZ=100), matching the real Talos format.
	// The Omni chart computes diff(new, old) to get per-interval usage, so values must always increase.
	// Each simulated 30-second interval contributes numCores * hz * intervalSeconds total jiffies.
	const intervalJiffies = 64.0 * 100.0 * float64(perfUpdateInterval/time.Second) //nolint:mnd

	userPct := 0.05 + rand.Float64()*0.25   //nolint:mnd
	systemPct := 0.02 + rand.Float64()*0.13 //nolint:mnd
	idlePct := 1.0 - userPct - systemPct

	if err := safe.WriterModify(ctx, r, perf.NewCPU(), func(res *perf.CPU) error {
		res.TypedSpec().CPUTotal.User += intervalJiffies * userPct
		res.TypedSpec().CPUTotal.System += intervalJiffies * systemPct
		res.TypedSpec().CPUTotal.Idle += intervalJiffies * idlePct
		res.TypedSpec().ContextSwitches += uint64(rand.IntN(50000)) + 10000 //nolint:mnd
		res.TypedSpec().ProcessCreated += uint64(rand.IntN(100))            //nolint:mnd
		res.TypedSpec().ProcessRunning = uint64(rand.IntN(8)) + 1           //nolint:mnd
		res.TypedSpec().SoftIrqTotal += uint64(rand.IntN(5000)) + 1000      //nolint:mnd

		return nil
	}); err != nil {
		return err
	}

	const totalMem = 64 * 1024 * 1024 * 1024

	usedMem := uint64(float64(totalMem) * (0.1 + rand.Float64()*0.2)) //nolint:mnd

	return safe.WriterModify(ctx, r, perf.NewMemory(), func(res *perf.Memory) error {
		res.TypedSpec().MemTotal = totalMem
		res.TypedSpec().MemUsed = usedMem
		res.TypedSpec().MemAvailable = totalMem - usedMem
		res.TypedSpec().Cached = usedMem / 4   //nolint:mnd
		res.TypedSpec().Buffers = usedMem / 8  //nolint:mnd
		res.TypedSpec().Active = usedMem / 2   //nolint:mnd
		res.TypedSpec().Inactive = usedMem / 4 //nolint:mnd
		res.TypedSpec().SwapTotal = 0
		res.TypedSpec().SwapFree = 0

		return nil
	})
}
