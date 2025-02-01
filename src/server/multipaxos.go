package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// Snapshot holds the snapshot data and the log index up to which the state is valid.
type Snapshot struct {
	LastIncludedIndex int               `json:"last_included_index"`
	State             map[string]string `json:"state"`
}

// MultiPaxos manages multiple SinglePaxos instances (i.e. a replicated log).
type MultiPaxos struct {
	mu              sync.Mutex
	NodeID          string
	Instances       map[int]*SinglePaxos // consensus instances (log entries)
	NextInstance    int                  // next log index to use
	StateMachine    *KVStore             // our replicated state machine

	// In-memory checkpoint information for log compaction.
	CheckpointIndex int       // highest log index that has been snapshotted
	SnapshotData    *Snapshot // latest snapshot of the state machine
}

// NewMultiPaxos creates a new MultiPaxos instance.
func NewMultiPaxos(nodeID string) *MultiPaxos {
	return &MultiPaxos{
		NodeID:          nodeID,
		Instances:       make(map[int]*SinglePaxos),
		StateMachine:    NewKVStore(),
		NextInstance:    0,
		CheckpointIndex: -1,
		SnapshotData:    nil,
	}
}

// StartConsensus starts a new consensus instance for a proposed value.
func (mp *MultiPaxos) StartConsensus(value *ProposalValue) {
	mp.mu.Lock()
	idx := mp.NextInstance
	mp.NextInstance++
	sp := NewSinglePaxos(idx)
	mp.Instances[idx] = sp
	mp.mu.Unlock()

	// In a full implementation, the consensus protocol would be executed across nodes.
	// Here we simulate immediate consensus:
	go mp.runInstance(sp, value)
}
func (mp *MultiPaxos) GetInstance(index int) *SinglePaxos {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	if instance, ok := mp.Instances[index]; ok {
		return instance
	}
	return nil
}

// SetInstance stores a consensus instance by index.
func (mp *MultiPaxos) SetInstance(index int, sp *SinglePaxos) {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	mp.Instances[index] = sp
}
// runInstance simulates a consensus instance: decide the value and apply it.
func (mp *MultiPaxos) runInstance(sp *SinglePaxos, value *ProposalValue) {
	sp.Decide(value)
	// Assume command format "SET key value"
	parts := strings.Split(value.Command, " ")
	if len(parts) == 3 && strings.ToUpper(parts[0]) == "SET" {
		mp.StateMachine.Set(parts[1], parts[2])
	}
	fmt.Printf("Node %s: Chose value for instance %d: %v\n", mp.NodeID, sp.InstanceID, value.Command)
}

// GetStateMachine returns the underlying KVStore.
func (mp *MultiPaxos) GetStateMachine() *KVStore {
	return mp.StateMachine
}

// TakeSnapshot creates an in-memory snapshot of the current state machine and compacts the log.
func (mp *MultiPaxos) TakeSnapshot() (*Snapshot, error) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	stateCopy, err := mp.StateMachine.Snapshot()
	if err != nil {
		return nil, err
	}
	// Use the latest instance index as the snapshot index.
	snap := &Snapshot{
		LastIncludedIndex: mp.NextInstance - 1,
		State:             stateCopy,
	}
	// Compact the log: remove instances with index <= LastIncludedIndex.
	for idx := range mp.Instances {
		if idx <= snap.LastIncludedIndex {
			delete(mp.Instances, idx)
		}
	}
	mp.CheckpointIndex = snap.LastIncludedIndex
	mp.SnapshotData = snap
	fmt.Printf("Node %s: Snapshot taken at index %d\n", mp.NodeID, snap.LastIncludedIndex)
	return snap, nil
}

// SaveSnapshotToEtcd takes a snapshot and stores it in etcd under the specified key.
func (mp *MultiPaxos) SaveSnapshotToEtcd(cli *clientv3.Client, key string) error {
	snap, err := mp.TakeSnapshot()
	if err != nil {
		return err
	}
	data, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = cli.Put(ctx, key, string(data))
	if err != nil {
		return err
	}
	fmt.Printf("Node %s: Snapshot saved to etcd under key %s\n", mp.NodeID, key)
	return nil
}

// LoadSnapshotFromEtcd loads a snapshot from etcd (if available) under the specified key.
func (mp *MultiPaxos) LoadSnapshotFromEtcd(cli *clientv3.Client, key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := cli.Get(ctx, key)
	if err != nil {
		return err
	}
	if len(resp.Kvs) == 0 {
		// No snapshot available.
		return nil
	}
	var snap Snapshot
	err = json.Unmarshal(resp.Kvs[0].Value, &snap)
	if err != nil {
		return err
	}
	mp.mu.Lock()
	defer mp.mu.Unlock()
	// Restore state machine state.
	mp.StateMachine.mu.Lock()
	mp.StateMachine.data = snap.State
	mp.StateMachine.mu.Unlock()

	mp.CheckpointIndex = snap.LastIncludedIndex
	mp.SnapshotData = &snap
	if mp.NextInstance < snap.LastIncludedIndex+1 {
		mp.NextInstance = snap.LastIncludedIndex + 1
	}
	// Compact the log.
	for idx := range mp.Instances {
		if idx <= snap.LastIncludedIndex {
			delete(mp.Instances, idx)
		}
	}
	fmt.Printf("Node %s: Loaded snapshot from etcd at index %d\n", mp.NodeID, snap.LastIncludedIndex)
	return nil
}
