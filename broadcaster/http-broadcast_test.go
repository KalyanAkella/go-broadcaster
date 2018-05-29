package broadcaster

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func BenchmarkHTTPBroadcast(b *testing.B) {
	b.ResetTimer()
	// b.RunParallel(...)
	for i := 0; i < b.N; i++ {
	}
}

func newListener(endpoint string) net.Listener {
	if l, err := net.Listen("tcp", endpoint); err != nil {
		panic(err)
	} else {
		return l
	}
}

func newBroadcastServer(handler http.HandlerFunc) *httptest.Server {
	aServer := httptest.NewUnstartedServer(handler)
	aServer.Listener = newListener(fmt.Sprintf("localhost:%s", BroadcastServerPort))
	aServer.Start()
	return aServer
}

func newServer(tag, endpoint string, res_chan chan<- string) *httptest.Server {
	aServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := fmt.Sprintf("%s Got Request", tag)
		res_chan <- response
		fmt.Fprint(w, response)
	}))
	aServer.Listener = newListener(endpoint)
	aServer.Start()
	return aServer
}

const (
	PrimaryTag          = "B2"
	BroadcastServerPort = "9090"
	NumRequests         = 10
)

var backendServers = map[string]string{
	"B1":       "localhost:9091",
	PrimaryTag: "localhost:9092",
	"B3":       "localhost:9093",
}

func readTag(response string) string {
	var tag string
	if _, err := fmt.Sscanf(response, "%s", &tag); err != nil {
		panic(err)
	}
	return tag
}

func httpGet(url string) string {
	if res, err := http.Get(url); err != nil {
		panic(err)
	} else {
		defer res.Body.Close()
		if res_bytes, err := ioutil.ReadAll(res.Body); err != nil {
			panic(err)
		} else {
			return string(res_bytes)
		}
	}
}

func TestHTTPBroadcast(t *testing.T) {
	// Given
	res_chan := make(chan string, len(backendServers))
	backends := make(map[EndPointId]EndPoint)
	for t, e := range backendServers {
		server := newServer(t, e, res_chan)
		defer server.Close()
		backends[t] = fmt.Sprintf("http://%s", e)
	}

	// When
	if broadcaster, err := NewBroadcaster(&BroadcastConfig{
		Backends: backends,
		Options: map[BroadcastOption]string{
			PORT:                     BroadcastServerPort,
			PRIMARY:                  PrimaryTag,
			RESPONSE_TIMEOUT_IN_SECS: "10",
		},
	}); err != nil {
		t.Fatal(err)
	} else {
		broadcast_server := newBroadcastServer(broadcaster.Handler)
		defer broadcast_server.Close()
	}

	// Then
	responded := make(map[string]int, len(backendServers))
	for t := range backendServers {
		responded[t] = 0
	}
	for i := 1; i <= NumRequests; i++ {
		broadcast_res := httpGet("http://localhost:9090")
		if primary_tag := readTag(broadcast_res); primary_tag != PrimaryTag {
			t.Errorf("Expected primary tag %s, Actual primary tag %s. Broadcast Response %s", PrimaryTag, primary_tag, broadcast_res)
		}
		for range backendServers {
			select {
			case msg := <-res_chan:
				responded[readTag(msg)]++
			default:
			}
		}
	}
	for k, v := range responded {
		if v < NumRequests {
			t.Errorf("No response from server with tag: %s", k)
		}
	}
}
