package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// NodeAPI is an interface that our Node implements so that REST handlers can obtain
// the KVStore (from the consensus engine) and other information.
type NodeAPI interface {
	IsLeader() bool
	GetLeaderRESTURL() string
	GetPaxosEngine() *MultiPaxos
}

// jsonResponse writes data as JSON with the proper content-type header.
func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// StartHTTPServer starts the REST API server.
func StartHTTPServer(port int, node NodeAPI) {
	mux := http.NewServeMux()

	// Get the list of keys (can be sequentially consistent)
	mux.HandleFunc("/kv/keys", func(w http.ResponseWriter, r *http.Request) {
		keys := node.GetPaxosEngine().GetStateMachine().ListKeys()
		jsonResponse(w, keys)
	})

	// Get the value of a single key
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

	// Get a range of keys: /kv/get-range?start=xxx&end=yyy
	mux.HandleFunc("/kv/get-range", func(w http.ResponseWriter, r *http.Request) {
		start := r.URL.Query().Get("start")
		end := r.URL.Query().Get("end")
		if start == "" || end == "" {
			http.Error(w, "Missing start or end parameter", http.StatusBadRequest)
			return
		}
		result := node.GetPaxosEngine().GetStateMachine().GetRange(start, end)
		jsonResponse(w, result)
	})

	// Update a single key; linearizable
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
		// For linearizability, the write must be processed by the leader.
		if node.IsLeader() {
			// Propose the operation via consensus.
			node.GetPaxosEngine().StartConsensus(&ProposalValue{Command: fmt.Sprintf("SET %s %s", key, value)})
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte("Proposal submitted"))
		} else {
			leaderURL := node.GetLeaderRESTURL()
			http.Error(w, "Not leader, forward to: "+leaderURL, http.StatusTemporaryRedirect)
		}
	})

	// Update a list of keys; linearizable.
	// Expect JSON body: {"keys": ["k1","k2"], "values": ["v1","v2"]}
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
			// For simplicity, process each key update as a separate consensus instance.
			for i, key := range req.Keys {
				cmd := fmt.Sprintf("SET %s %s", key, req.Values[i])
				node.GetPaxosEngine().StartConsensus(&ProposalValue{Command: cmd})
			}
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte("Multiple keys proposal submitted"))
		} else {
			leaderURL := node.GetLeaderRESTURL()
			http.Error(w, "Not leader, forward to: "+leaderURL, http.StatusTemporaryRedirect)
		}
	})

	// Read-Modify-Write (RMW) on a key; linearizable.
	// Expect JSON body: {"key": "xxx", "operation": "append" or "increment", "operand": "value"}
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
			// For this example, we implement two operations: "increment" (for numeric values) and "append" (for strings).
			current, _ := node.GetPaxosEngine().GetStateMachine().Get(req.Key)
			var newValue string
			switch strings.ToLower(req.Operation) {
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
			// Propose the RMW as a consensus operation.
			node.GetPaxosEngine().StartConsensus(&ProposalValue{Command: fmt.Sprintf("SET %s %s", req.Key, newValue)})
			w.WriteHeader(http.StatusAccepted)
			jsonResponse(w, map[string]string{
				"key":   req.Key,
				"value": newValue,
			})
		} else {
			leaderURL := node.GetLeaderRESTURL()
			http.Error(w, "Not leader, forward to: "+leaderURL, http.StatusTemporaryRedirect)
		}
	})

	http.ListenAndServe(":"+strconv.Itoa(port), mux)
}
