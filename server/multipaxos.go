package server

import (
	"fmt"
	"sync"
)

type MultiPaxos struct {
	mu           sync.Mutex
	NodeID       string
	Instances    map[int]*SinglePaxos
	NextInstance int
	StateMachine *KVStore
}

func NewMultiPaxos(nodeID string) *MultiPaxos {
	return &MultiPaxos{
		NodeID:       nodeID,
		Instances:    make(map[int]*SinglePaxos),
		StateMachine: NewKVStore(),
	}
}

func (mp *MultiPaxos) StartConsensus(value *ProposalValue) {
	mp.mu.Lock()
	idx := mp.NextInstance
	mp.NextInstance++
	sp := NewSinglePaxos(idx)
	mp.Instances[idx] = sp
	mp.mu.Unlock()

	go mp.runInstance(sp, value)
}

func (mp *MultiPaxos) runInstance(sp *SinglePaxos, value *ProposalValue) {
	sp.Decide(value)
	mp.StateMachine.Apply(value.Command)
	fmt.Printf("Node %s: Chose value for instance %d: %v\n", mp.NodeID, sp.InstanceID, value.Command)
}

func (mp *MultiPaxos) GetInstance(index int) *SinglePaxos {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	if instance, ok := mp.Instances[index]; ok {
		return instance
	}
	return nil
}

func (mp *MultiPaxos) SetInstance(index int, sp *SinglePaxos) {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	mp.Instances[index] = sp
}

func (mp *MultiPaxos) GetStateMachine() *KVStore {
	return mp.StateMachine
}
