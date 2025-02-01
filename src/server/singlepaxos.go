package server

import (
	"fmt"
	"sync"
)

type ProposalValue struct {
	Command string
}

type SinglePaxos struct {
	mu                sync.Mutex
	InstanceID        int
	ChosenValue       *ProposalValue
	IsChosen          bool
	LastPromisedRound int
	AcceptedRound     int
	AcceptedValue     *ProposalValue
}

func NewSinglePaxos(instanceID int) *SinglePaxos {
	return &SinglePaxos{
		InstanceID: instanceID,
	}
}

func (sp *SinglePaxos) Prepare(round int) (bool, int, *ProposalValue) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if round >= sp.LastPromisedRound {
		sp.LastPromisedRound = round
		return true, sp.AcceptedRound, sp.AcceptedValue
	}
	return false, sp.AcceptedRound, sp.AcceptedValue
}

func (sp *SinglePaxos) Accept(round int, value *ProposalValue) bool {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if round >= sp.LastPromisedRound {
		sp.LastPromisedRound = round
		sp.AcceptedRound = round
		sp.AcceptedValue = value
		return true
	}
	return false
}

func (sp *SinglePaxos) Decide(value *ProposalValue) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	sp.ChosenValue = value
	sp.IsChosen = true
	fmt.Printf("[SinglePaxos %d] Value decided: %v\n", sp.InstanceID, value.Command)
}
