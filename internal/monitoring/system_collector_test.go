// Licensed to LinDB under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. LinDB licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package monitoring

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"go.uber.org/mock/gomock"

	"github.com/lindb/lindb/internal/linmetric"
	"github.com/lindb/lindb/metrics"
	"github.com/lindb/lindb/models"
)

func Test_NewSystemCollector(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithCancel(context.TODO())

	collector := NewSystemCollector(
		ctx,
		"/tmp",
		metrics.NewSystemStatistics(linmetric.StorageRegistry),
	)

	go func() {
		time.Sleep(time.Second)
		cancel()
	}()

	// collect system monitoring stat
	collector.Run()
}

func Test_SystemCollector_Collect(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	collector := NewSystemCollector(
		ctx,
		"/tmp",
		metrics.NewSystemStatistics(linmetric.BrokerRegistry),
	)

	collector.MemoryStatGetter = func() (*mem.VirtualMemoryStat, error) {
		return nil, fmt.Errorf("error")
	}
	collector.collect()
	collector.MemoryStatGetter = mem.VirtualMemory

	collector.CPUStatGetter = func() (*models.CPUStat, error) {
		return nil, fmt.Errorf("error")
	}
	collector.collect()
	collector.CPUStatGetter = GetCPUStat

	collector.DiskUsageStatGetter = func(ctx context.Context, path string) (*disk.UsageStat, error) {
		return nil, fmt.Errorf("error")
	}
	collector.collect()
	collector.DiskUsageStatGetter = disk.UsageWithContext

	collector.NetStatGetter = func(ctx context.Context) (stats []net.IOCountersStat, err error) {
		return nil, fmt.Errorf("error")
	}
	collector.collect()
	collector.NetStatGetter = GetNetStat

	collector.collect()
	collector.collect()
}
