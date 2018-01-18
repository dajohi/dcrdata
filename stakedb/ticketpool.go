package stakedb

import (
	"fmt"
	"sync"

	"github.com/asdine/storm"
	"github.com/decred/dcrd/chaincfg/chainhash"
)

type TicketPool struct {
	*sync.RWMutex
	cursor int64
	pool   map[chainhash.Hash]struct{}
	tip    int64
	diffs  []PoolDiff
	diffDB *storm.DB
}

type PoolDiff struct {
	In  []chainhash.Hash
	Out []chainhash.Hash
}

type PoolDiffDBItem struct {
	Height   int64 `storm:"id"`
	PoolDiff `storm:"inline"`
}

func NewTicketPool(dbFile string) (*TicketPool, error) {
	// Open ticket pool diffs database
	db, err := storm.Open(dbFile)
	if err != nil {
		return nil, fmt.Errorf("failed storm.Open: %v", err)
	}

	// Load all diffs
	var poolDiffs []PoolDiffDBItem
	err = db.AllByIndex("Height", &poolDiffs)
	if err != nil {
		return nil, fmt.Errorf("failed (*storm.DB).AllByIndex: %v", err)
	}
	diffs := make([]PoolDiff, len(poolDiffs))
	for i := range poolDiffs {
		diffs[i] = poolDiffs[i].PoolDiff
	}

	// Construct TicketPool with loaded diffs and diff DB
	return &TicketPool{
		RWMutex: new(sync.RWMutex),
		pool:    make(map[chainhash.Hash]struct{}),
		diffs:   diffs,             // make([]PoolDiff, 0, 100000),
		tip:     int64(len(diffs)), // number of blocks connected over genesis
		diffDB:  db,
	}, nil
}

func (tp *TicketPool) Close() error {
	return tp.diffDB.Close()
}

func (tp *TicketPool) Tip() int64 {
	tp.RLock()
	defer tp.RUnlock()
	return tp.tip
}

func (tp *TicketPool) Cursor() int64 {
	tp.RLock()
	defer tp.RUnlock()
	return tp.cursor
}

func (tp *TicketPool) append(diff *PoolDiff) {
	tp.tip++
	tp.diffs = append(tp.diffs, *diff)
}

func (tp *TicketPool) trim() int64 {
	if tp.tip == 0 || len(tp.diffs) == 0 {
		return tp.tip
	}
	tp.tip--
	newMaxCursor := tp.maxCursor()
	if tp.cursor > newMaxCursor {
		tp.retreatTo(newMaxCursor)
	}
	tp.diffs = tp.diffs[:len(tp.diffs)-1]
	return tp.tip
}

func (tp *TicketPool) Trim() int64 {
	tp.Lock()
	defer tp.Unlock()
	return tp.trim()
}

func (tp *TicketPool) storeDiff(diff *PoolDiff, height int64) error {
	d := &PoolDiffDBItem{
		Height:   height,
		PoolDiff: *diff,
	}
	return tp.diffDB.Save(d)
}

func (tp *TicketPool) fetchDiff(height int64) (*PoolDiffDBItem, error) {
	var diff PoolDiffDBItem
	err := tp.diffDB.One("Height", height, &diff)
	return &diff, err
}

func (tp *TicketPool) Append(diff *PoolDiff, height int64) error {
	if height != tp.tip+1 {
		return fmt.Errorf("block height %d does not build on %d", height, tp.tip)
	}
	tp.Lock()
	defer tp.Unlock()
	tp.append(diff)
	return tp.storeDiff(diff, height)
}

func (tp *TicketPool) AppendAndAdvancePool(diff *PoolDiff, height int64) error {
	if height != tp.tip+1 {
		return fmt.Errorf("block height %d does not build on %d", height, tp.tip)
	}
	tp.Lock()
	defer tp.Unlock()
	tp.append(diff)
	if err := tp.storeDiff(diff, height); err != nil {
		return err
	}
	return tp.advance()
}

func (tp *TicketPool) currentPool() ([]chainhash.Hash, int64) {
	poolSize := len(tp.pool)
	// allocate space for all the ticket hashes, but use append to avoid the
	// slice initialization having to zero initialize all of the arrays.
	pool := make([]chainhash.Hash, 0, poolSize)
	for h := range tp.pool {
		pool = append(pool, h)
	}
	return pool, tp.cursor
}

func (tp *TicketPool) CurrentPool() ([]chainhash.Hash, int64) {
	tp.RLock()
	defer tp.RUnlock()
	return tp.currentPool()
}

func (tp *TicketPool) CurrentPoolSize() int {
	tp.RLock()
	defer tp.RUnlock()
	return len(tp.pool)
}

func (tp *TicketPool) Pool(height int64) ([]chainhash.Hash, error) {
	tp.Lock()
	defer tp.Unlock()

	if height > tp.tip {
		return nil, fmt.Errorf("block height %d is not connected yet, tip is %d", height, tp.tip)
	}

	for height > tp.cursor {
		if err := tp.advance(); err != nil {
			return nil, err
		}
	}
	for tp.cursor > height {
		if err := tp.retreat(); err != nil {
			return nil, err
		}
	}
	p, _ := tp.currentPool()
	return p, nil
}

func (tp *TicketPool) advance() error {
	if tp.cursor > tp.maxCursor() {
		return fmt.Errorf("cursor at tip, unable to advance")
	}

	diffToNext := tp.diffs[tp.cursor]
	initPoolSize := len(tp.pool)
	expectedFinalSize := initPoolSize + len(diffToNext.In) - len(diffToNext.Out)

	tp.applyDiff(diffToNext.In, diffToNext.Out)
	tp.cursor++

	if len(tp.pool) != expectedFinalSize {
		return fmt.Errorf("pool size is %d, expected %d", len(tp.pool), expectedFinalSize)
	}

	return nil
}

func (tp *TicketPool) advanceTo(height int64) error {
	if height > tp.tip {
		return fmt.Errorf("cannot advance past tip")
	}
	for height > tp.cursor {
		if err := tp.advance(); err != nil {
			return err
		}
	}
	return nil
}

func (tp *TicketPool) AdvanceToTip() error {
	return tp.advanceTo(tp.tip)
}

func (tp *TicketPool) retreat() error {
	if tp.cursor == 0 {
		return fmt.Errorf("cursor at genesis, unable to retreat")
	}

	diffFromPrev := tp.diffs[tp.cursor-1]
	initPoolSize := len(tp.pool)
	expectedFinalSize := initPoolSize - len(diffFromPrev.In) + len(diffFromPrev.Out)

	tp.applyDiff(diffFromPrev.Out, diffFromPrev.In)
	tp.cursor--

	if len(tp.pool) != expectedFinalSize {
		return fmt.Errorf("pool size is %d, expected %d", len(tp.pool), expectedFinalSize)
	}
	return nil
}

func (tp *TicketPool) maxCursor() int64 {
	if tp.tip == 0 {
		return 0
	}
	return tp.tip - 1
}

func (tp *TicketPool) retreatTo(height int64) error {
	if height < 0 || height > tp.tip {
		return fmt.Errorf("Invalid destination cursor %d", height)
	}
	for tp.cursor > height {
		if err := tp.retreat(); err != nil {
			return err
		}
	}
	return nil
}

func (tp *TicketPool) applyDiff(in, out []chainhash.Hash) {
	initsize := len(tp.pool)
	for i := range in {
		tp.pool[in[i]] = struct{}{}
	}
	endsize := len(tp.pool)
	if endsize != initsize+len(in) {
		log.Warnf("pool grew by %d instead of %d", endsize-initsize, len(in))
	}
	initsize = endsize
	for i := range out {
		delete(tp.pool, out[i])
	}
	endsize = len(tp.pool)
	if endsize != initsize-len(out) {
		log.Warnf("pool shrank by %d instead of %d", initsize-endsize, len(out))
	}
}
