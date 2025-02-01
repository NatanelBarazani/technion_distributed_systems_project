package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// NodeAPI is an interface that our Node implements.
type NodeAPI interface {
	IsLeader() bool
	GetLeaderRESTURL() string
	GetPaxosEngine() *MultiPaxos
	GetLocalRESTURL() string
}

// jsonResponse writes data as JSON.
func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// StartHTTPServer starts the REST API server.
func StartHTTPServer(port int, node NodeAPI) {
	mux := http.NewServeMux()

	// Replication endpoint: used by the leader to push decisions.
	mux.HandleFunc("/replicate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			InstanceID int    `json:"instance_id"`
			Command    string `json:"command"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
		// Check if this instance is already applied.
		existing := node.GetPaxosEngine().GetInstance(req.InstanceID)
		if existing != nil && existing.IsChosen {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Already applied"))
			return
		}
		// Simulate immediate consensus on the replicated decision.
		newInstance := NewSinglePaxos(req.InstanceID)
		newInstance.Decide(&ProposalValue{Command: req.Command})
		node.GetPaxosEngine().SetInstance(req.InstanceID, newInstance)
		// Apply the command to the state machine.
		parts := strings.Split(req.Command, " ")
		if len(parts) == 3 && strings.ToUpper(parts[0]) == "SET" {
			node.GetPaxosEngine().GetStateMachine().Set(parts[1], parts[2])
		}
		fmt.Printf("Node %s: Replicated decision for instance %d: %s\n", node.GetLocalRESTURL(), req.InstanceID, req.Command)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Replication applied"))
	})

	// Get list of keys.
	mux.HandleFunc("/kv/keys", func(w http.ResponseWriter, r *http.Request) {
		keys := node.GetPaxosEngine().GetStateMachine().ListKeys()
		jsonResponse(w, keys)
	})

	// Get value for a key.
	mux.HandleFunc("/kv/get", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		if key == "" {
			http.Error(w, "Missing key parameter", http.StatusBadRequest)
			return
		}
		value, ok := node.GetPaxosEngine().GetStateMachine().Get(key)
		if !ok {
			http.Error(w, "Key not found", http.StatusNotFound)
			return
		}
		jsonResponse(w, map[string]string{"key": key, "value": value})
	})

	// Update a key.
	mux.HandleFunc("/kv/set", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		key := r.URL.Query().Get("key")
		value := r.URL.Query().Get("value")
		if key == "" {
			http.Error(w, "Missing key parameter", http.StatusBadRequest)
			return
		}
		if node.IsLeader() {
			cmd := fmt.Sprintf("SET %s %s", key, value)
			node.GetPaxosEngine().StartConsensus(&ProposalValue{Command: cmd})
			instanceID := node.GetPaxosEngine().NextInstance - 1
			// Replicate the decision.
			node.(*Node).OnDecision(instanceID, cmd)
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte("Proposal submitted"))
		} else {
			leaderURL := node.GetLeaderRESTURL()
			http.Error(w, "Not leader, forward to: "+leaderURL, http.StatusTemporaryRedirect)
		}
	})

	// Update multiple keys.
	mux.HandleFunc("/kv/set-multi", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Keys   []string `json:"keys"`
			Values []string `json:"values"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
		if len(req.Keys) != len(req.Values) {
			http.Error(w, "Keys and values length mismatch", http.StatusBadRequest)
			return
		}
		if node.IsLeader() {
			for i, key := range req.Keys {
				cmd := fmt.Sprintf("SET %s %s", key, req.Values[i])
				node.GetPaxosEngine().StartConsensus(&ProposalValue{Command: cmd})
				instanceID := node.GetPaxosEngine().NextInstance - 1
				node.(*Node).OnDecision(instanceID, cmd)
			}
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte("Multiple keys proposal submitted"))
		} else {
			leaderURL := node.GetLeaderRESTURL()
			http.Error(w, "Not leader, forward to: "+leaderURL, http.StatusTemporaryRedirect)
		}
	})

	// Read-Modify-Write operation.
	mux.HandleFunc("/kv/rmw", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Key       string `json:"key"`
			Operation string `json:"operation"`
			Operand   string `json:"operand"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
		if req.Key == "" || req.Operation == "" {
			http.Error(w, "Missing key or operation", http.StatusBadRequest)
			return
		}
		if node.IsLeader() {
			current, _ := node.GetPaxosEngine().GetStateMachine().Get(req.Key)
			var newValue string
			switch req.Operation {
			case "increment":
				currInt, err := strconv.Atoi(current)
				if err != nil {
					currInt = 0
				}
				operandInt, err := strconv.Atoi(req.Operand)
				if err != nil {
					operandInt = 1
				}
				newValue = strconv.Itoa(currInt + operandInt)
			case "append":
				newValue = current + req.Operand
			default:
				http.Error(w, "Unsupported operation", http.StatusBadRequest)
				return
			}
			cmd := fmt.Sprintf("SET %s %s", req.Key, newValue)
			node.GetPaxosEngine().StartConsensus(&ProposalValue{Command: cmd})
			instanceID := node.GetPaxosEngine().NextInstance - 1
			node.(*Node).OnDecision(instanceID, cmd)
			w.WriteHeader(http.StatusAccepted)
			jsonResponse(w, map[string]string{"key": req.Key, "value": newValue})
		} else {
			leaderURL := node.GetLeaderRESTURL()
			http.Error(w, "Not leader, forward to: "+leaderURL, http.StatusTemporaryRedirect)
		}
	})

	http.ListenAndServe(":"+strconv.Itoa(port), mux)
}
