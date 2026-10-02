// A single-file request/response example for the source structure explorer.
// stateLoop owns data; writeLoop serializes APPEND and remembers request IDs.
package main

import "fmt"

type stateRequest struct {
	operation string
	key       string
	value     string
	reply     chan string
}

type writeRequest struct {
	id        int
	operation string
	key       string
	value     string
	reply     chan string
}

type KVStore struct {
	stateChan  chan stateRequest
	writeChan  chan writeRequest
	terminated chan struct{}
	stateDone  chan struct{}
	writeDone  chan struct{}
}

func newKVStore() *KVStore {
	store := &KVStore{
		stateChan:  make(chan stateRequest),
		writeChan:  make(chan writeRequest),
		terminated: make(chan struct{}),
		stateDone:  make(chan struct{}),
		writeDone:  make(chan struct{}),
	}
	go store.stateLoop()
	go store.writeLoop()
	return store
}

func (store *KVStore) stateLoop() {
	defer close(store.stateDone)
	data := make(map[string]string)
	for {
		select {
		case request := <-store.stateChan:
			switch request.operation {
			case "GET":
				request.reply <- data[request.key]
			case "SET":
				data[request.key] = request.value
				request.reply <- data[request.key]
			}
		case <-store.terminated:
			return
		}
	}
}

func (store *KVStore) writeLoop() {
	defer close(store.writeDone)
	completed := make(map[int]string)
	for {
		select {
		case request := <-store.writeChan:
			if previous, exists := completed[request.id]; exists {
				request.reply <- previous
				continue
			}
			switch request.operation {
			case "APPEND":
				getResponse := make(chan string)
				store.stateChan <- stateRequest{operation: "GET", key: request.key, reply: getResponse}
				oldValue := <-getResponse
				newValue := oldValue + request.value
				store.diskWrite(request.key, newValue)
				setResponse := make(chan string)
				store.stateChan <- stateRequest{operation: "SET", key: request.key, value: newValue, reply: setResponse}
				stored := <-setResponse
				completed[request.id] = stored
				request.reply <- stored
			default:
				request.reply <- "unsupported operation"
			}
		case <-store.terminated:
			return
		}
	}
}

// This demonstration logs the disk operation; it does not write any files.
func (store *KVStore) diskWrite(key string, value string) {
	fmt.Println("disk:", key, "=", value)
}

func (store *KVStore) Operation(id int, operation string, key string, value string) string {
	reply := make(chan string)
	if operation == "GET" {
		store.stateChan <- stateRequest{operation: "GET", key: key, reply: reply}
	} else {
		store.writeChan <- writeRequest{id: id, operation: operation, key: key, value: value, reply: reply}
	}
	return <-reply
}

// Close is called once, after all Operation calls return. Both background
// goroutines acknowledge shutdown so the example does not depend on sleeps.
func (store *KVStore) Close() {
	close(store.terminated)
	<-store.stateDone
	<-store.writeDone
}

func main() {
	store := newKVStore()
	fmt.Println("APPEND:", store.Operation(1, "APPEND", "greeting", "Hello"))
	fmt.Println("retry:", store.Operation(1, "APPEND", "greeting", "Hello"))
	fmt.Println("APPEND:", store.Operation(2, "APPEND", "greeting", " Go"))
	fmt.Println("GET:", store.Operation(3, "GET", "greeting", ""))
	store.Close()
	fmt.Println("stopped")
}
