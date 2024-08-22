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

package kv

import (
	"errors"
	"fmt"
	"time"

	"github.com/lindb/common/pkg/logger"

	"github.com/lindb/lindb/kv/table"
	"github.com/lindb/lindb/kv/version"
	"github.com/lindb/lindb/metrics"
)

//go:generate mockgen -source ./compact_job.go -destination=./compact_job_mock.go -package kv

// CompactJob represents the compact job which does merge sst files
type CompactJob interface {
	// Run runs compact logic
	Run() error
}

// compactJob represents the compaction job, merges input files
type compactJob struct {
	family    Family
	state     *compactionState
	newMerger NewMerger
	rollup    Rollup // if rollup isn't nil, need do rollup job

	compactType string
}

// newCompactJob creates a compaction job
func newCompactJob(family Family, state *compactionState, rollup Rollup) CompactJob {
	cType := "merge"
	if rollup != nil {
		cType = "rollup"
	}
	return &compactJob{
		family:      family,
		newMerger:   family.getNewMerger(),
		state:       state,
		rollup:      rollup,
		compactType: cType,
	}
}

// Run runs compact job
func (c *compactJob) Run() error {
	startTime := time.Now()
	metrics.CompactStatistics.Compacting.WithTagValues(c.compactType).Incr()
	defer func() {
		metrics.CompactStatistics.Compacting.WithTagValues(c.compactType).Decr()
		metrics.CompactStatistics.Duration.WithTagValues(c.compactType).UpdateSince(startTime)
	}()
	compaction := c.state.compaction
	switch {
	case c.rollup == nil && compaction.IsTrivialMove():
		// compact job can move file
		c.moveCompaction()
	default:
		if err := c.mergeCompaction(); err != nil {
			metrics.CompactStatistics.Failure.WithTagValues(c.compactType).Incr()
			return err
		}
	}
	return nil
}

// moveCompaction moves low level file to up level, just does metadata change
func (c *compactJob) moveCompaction() {
	startTime := time.Now()
	compaction := c.state.compaction
	kvLogger.Info("starting compaction job, just move file to next level",
		logger.String("family", c.family.familyInfo()), logger.String("type", c.compactType))
	// move file to next level
	fileMeta := compaction.GetLevelFiles()[0]
	level := compaction.GetLevel()
	compaction.DeleteFile(level, fileMeta.GetFileNumber())
	compaction.AddFile(level+1, fileMeta)
	c.family.commitEditLog(compaction.GetEditLog())

	elapsed := time.Since(startTime)
	kvLogger.Info("finish move file compaction",
		logger.String("family", c.family.familyInfo()),
		logger.String("type", c.compactType),
		logger.String("cost", elapsed.String()),
	)
}

// mergeCompaction merges input files to up level
func (c *compactJob) mergeCompaction() (err error) {
	startTime := time.Now()
	kvLogger.Info("starting compaction job, do merge compaction",
		logger.String("family", c.family.familyInfo()), logger.String("type", c.compactType))
	defer func() {
		// cleanup compaction context, include temp pending output files
		c.cleanupCompaction()

		elapsed := time.Since(startTime)
		kvLogger.Info("finish merge file compaction",
			logger.String("family", c.family.familyInfo()),
			logger.String("type", c.compactType),
			logger.String("cost", elapsed.String()),
		)
	}()

	// do merge logic
	if err := c.doMerge(); err != nil {
		return err
	}
	// if merge success install compaction results into manifest
	c.installCompactionResults()
	return nil
}

// doMerge merges the input files based on merger interface which need use implements
func (c *compactJob) doMerge() error {
	merger, err := c.newMerger(c.newCompactFlusher())
	if err != nil {
		return err
	}
	it, err := c.makeInputIterator()
	if err != nil {
		return err
	}
	if c.rollup != nil {
		merger.Init(map[string]interface{}{RollupContext: c.rollup})
	}

	var needMerge [][]byte
	var previousKey uint32
	start := true
	for it.HasNext() {
		key := it.Key()
		value := it.Value()
		switch {
		case start || key == previousKey:
			// if start or same keys, append to need merge slice
			needMerge = append(needMerge, value)
			start = false
		case key != previousKey:
			// FIXME: stone1100 merge data maybe is one block

			// 1. if new key != previous key do merge logic based on user define
			if err := merger.Merge(previousKey, needMerge); err != nil {
				return err
			}
			// 2. prepare next merge loop
			// init value for next loop
			needMerge = needMerge[:0]
			// add value to need merge slice
			needMerge = append(needMerge, value)
		}
		// set previous merge key
		previousKey = key
	}

	// if has pending merge values after iterator, need do merge
	if len(needMerge) > 0 {
		if err := merger.Merge(previousKey, needMerge); err != nil {
			return err
		}
	}
	// if it has store builder opened, need close it
	if c.state.builder != nil {
		if err := c.finishCompactionOutputFile(); err != nil {
			return err
		}
	}
	return nil
}

// installCompactionResults installs compactions results.
// 1. mark input files is deletion which compaction job picked.
// 2. add output files to up level.
// 3. commit edit log for manifest.
func (c *compactJob) installCompactionResults() {
	if c.rollup == nil {
		// if it does compact job, need mark compaction input files for deletion
		c.state.compaction.MarkInputDeletes()
	}
	// adds compaction outputs
	level := c.state.compaction.GetLevel()
	if c.rollup == nil {
		level++ // compact job need add level
	}
	for _, output := range c.state.outputs {
		c.state.compaction.AddFile(level, output)
	}
	c.family.commitEditLog(c.state.compaction.GetEditLog())
}

// makeInputIterator makes a merged iterator by compaction pick input files
func (c *compactJob) makeInputIterator() (table.Iterator, error) {
	var its []table.Iterator
	for which := 0; which < 2; which++ {
		files := c.state.compaction.GetInputs()[which]
		if len(files) > 0 {
			for _, fileMeta := range files {
				reader, err := c.state.snapshot.GetReader(fileMeta.GetFileNumber())
				if err != nil {
					return nil, err
				}
				its = append(its, reader.Iterator())
			}
		}
	}
	return table.NewMergedIterator(its), nil
}

// openCompactionOutputFile opens a new compaction store build, and adds the file number into pending output
func (c *compactJob) openCompactionOutputFile() error {
	builder, err := c.family.newTableBuilder()
	if err != nil {
		return err
	}
	c.state.builder = builder
	return nil
}

// finishCompactionOutputFile closes current store builder, then generates a new file into edit log
func (c *compactJob) finishCompactionOutputFile() (err error) {
	builder := c.state.builder
	// finally, need cleanup store build if no error
	defer func() {
		if err == nil {
			c.state.builder = nil
		}
	}()
	if builder == nil {
		return errors.New("store build is nil")
	}
	if builder.Count() == 0 {
		// if no data after compact
		return err
	}
	if err = builder.Close(); err != nil {
		return fmt.Errorf("close table builder error when compaction job, error:%w", err)
	}
	fileMeta := version.NewFileMeta(builder.FileNumber(), builder.MinKey(), builder.MaxKey(), builder.Size())
	c.state.addOutputFile(fileMeta)
	return err
}

// cleanupCompaction cleanups the compaction context, such as remove pending output files etc.
func (c *compactJob) cleanupCompaction() {
	if c.state.builder != nil {
		currentFileNumber := c.state.builder.FileNumber()
		if err := c.state.builder.Abandon(); err != nil {
			kvLogger.Warn("abandon store build error when do compact job",
				logger.String("family", c.family.familyInfo()), logger.String("type", c.compactType),
				logger.Int64("file", currentFileNumber.Int64()))
		}
		c.family.removePendingOutput(currentFileNumber)
	}
	for _, output := range c.state.outputs {
		c.family.removePendingOutput(output.GetFileNumber())
	}
}

// newCompactFlusher creates a new flusher for compacting
// there are 2 strategies for flushing: streaming flush and buffer flush
func (c *compactJob) newCompactFlusher() Flusher {
	return &compactFlusher{compactJob: c}
}

// compactFlusher wraps the kv builder, implements Flusher
// provides stream writer and add method
type compactFlusher struct {
	compactJob   *compactJob
	streamWriter table.StreamWriter // lazy initialized
}

func (cf *compactFlusher) StreamWriter() (table.StreamWriter, error) {
	if cf.streamWriter != nil {
		return cf.streamWriter, nil
	}
	// ensure builder is created
	if err := cf.beforeAdd(); err != nil {
		return nil, err
	}
	sw := cf.compactJob.state.builder.StreamWriter()
	// hooks stream writer with compaction processing checkers
	cf.streamWriter = &compactFlusherStreamWriter{
		compactFlusher: cf,
		StreamWriter:   sw,
	}
	return cf.streamWriter, nil
}

func (cf *compactFlusher) beforeAdd() error {
	// generates output file number and creates store build if necessary
	if cf.compactJob.state.builder == nil {
		if err := cf.compactJob.openCompactionOutputFile(); err != nil {
			return err
		}
	}
	return nil
}

func (cf *compactFlusher) afterAdd() error {
	// close current store build's file if it is big enough
	if cf.compactJob.state.builder.Size() >= cf.compactJob.state.maxFileSize {
		if err := cf.compactJob.finishCompactionOutputFile(); err != nil {
			return err
		}
	}
	return nil
}

// Add adds new k/v pair into new store build,
// if store builder is nil need create a new store builder,
// if file size > max file limit, closes current builder.
func (cf *compactFlusher) Add(key uint32, value []byte) error {
	if len(value) == 0 {
		return nil
	}
	// generates output file number and creates store build if necessary
	if err := cf.beforeAdd(); err != nil {
		return err
	}
	// add key/value into store builder
	if err := cf.compactJob.state.builder.Add(key, value); err != nil {
		return err
	}
	if err := cf.afterAdd(); err != nil {
		return err
	}
	return nil
}

func (cf *compactFlusher) Sequence(_ int32, _ int64) {
	// do nothing
}

func (cf *compactFlusher) Commit() error {
	panic("Commit is not allowed to call for CompactFlusher")
}

func (cf *compactFlusher) Release() {
	panic("Release is not allowed to call for CompactFlusher")
}

// compactFlusherStreamWriter wraps stream writer with write check
type compactFlusherStreamWriter struct {
	compactFlusher *compactFlusher
	table.StreamWriter
}

// Commit checks if build's file if it is big enough
func (cfsw *compactFlusherStreamWriter) Commit() error {
	// table's StreamWriter Commit won't raise error
	_ = cfsw.StreamWriter.Commit()
	return cfsw.compactFlusher.afterAdd()
}
