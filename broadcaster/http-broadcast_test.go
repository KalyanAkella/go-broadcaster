package broadcaster

import (
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newListener(endpoint string) net.Listener {
	if l, err := net.Listen("tcp", endpoint); err != nil {
		log.Fatal(err)
		return nil
	} else {
		return l
	}
}

func newBroadcastServer(handler http.HandlerFunc) *httptest.Server {
	aServer := httptest.NewUnstartedServer(handler)
	aServer.Listener = newListener(fmt.Sprintf("localhost:%d", BroadcastServerPort))
	aServer.Start()
	return aServer
}

func newServer(tag, endpoint string) *httptest.Server {
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
	BroadcastServerPort = 9090
	NumRequests         = 10
)

func readTag(response string) string {
	var tag string
	if _, err := fmt.Sscanf(response, "%s", &tag); err != nil {
		log.Fatal(err)
	}
	return tag
}

func httpGet(url string) (string, int) {
	if res, err := http.Get(url); err != nil {
		log.Fatal(err)
		return "", -1
	} else {
		defer res.Body.Close()
		if res_bytes, err := ioutil.ReadAll(res.Body); err != nil {
			log.Fatal(err)
			return "", -1
		} else {
			return string(res_bytes), res.StatusCode
		}
	}
}

var broadcast_server *httptest.Server
var res_chan chan string
var backends map[string]*httptest.Server
var backendServers map[string]string

func startBackendServers() {
	backends = make(map[string]*httptest.Server)
	for t, e := range backendServers {
		backends[t] = newServer(t, e)
	}
}

func startBroadcastServer() {
	servers := make(map[string]string, len(backendServers))
	for t, e := range backendServers {
		servers[t] = fmt.Sprintf("http://%s", e)
	}
	if broadcaster, err := NewBroadcaster(&BroadcastConfig{
		Backends: servers,
		Options: &BroadcastOptions{
			Port:            BroadcastServerPort,
			PrimaryEndpoint: PrimaryTag,
			LogLevel:        ERROR,
		},
	}); err != nil {
		log.Fatal(err)
	} else {
		broadcast_server = newBroadcastServer(broadcaster.Handler)
	}
}

func setup() {
	startBackendServers()
	startBroadcastServer()
}

func teardown() {
	shutdownBackend(broadcast_server)
	for _, backend := range backends {
		shutdownBackend(backend)
	}
}

func shutdownBackend(backend *httptest.Server) {
	backend.CloseClientConnections()
	backend.Close()
}

func TestHTTPBroadcastWithFailureResponse(t *testing.T) {
	backendServers = make(map[string]string)
	backendServers["B1"] = "localhost:9094"
	backendServers[PrimaryTag] = "localhost:9095"
	setup()
	defer teardown()
	shutdownBackend(backends[PrimaryTag])
	_, status_code := httpGet("http://localhost:9090")
	assertStatusCode(t, status_code, http.StatusServiceUnavailable)
}

func TestHTTPBroadcastWithSuccessResponse(t *testing.T) {
	backendServers = make(map[string]string)
	backendServers["B1"] = "localhost:9091"
	backendServers[PrimaryTag] = "localhost:9092"
	backendServers["B3"] = "localhost:9093"
	setup()
	defer teardown()
	for i := 1; i <= NumRequests; i++ {
		res_chan = make(chan string, len(backendServers))
		broadcast_res, status_code := httpGet("http://localhost:9090")
		assertStatusCode(t, status_code, http.StatusOK)
		assertForPrimaryResponse(t, broadcast_res)
		waitForSecondaryResponses(res_chan)
	}
}

func BenchmarkHTTPBroadcast(b *testing.B) {
	backendServers = make(map[string]string)
	backendServers["B1"] = "localhost:9096"
	backendServers[PrimaryTag] = "localhost:9097"
	backendServers["B3"] = "localhost:9098"
	setup()
	defer teardown()
	b.ResetTimer()
	for i := 1; i <= b.N; i++ {
		res_chan = make(chan string, len(backendServers))
		broadcast_res, status_code := httpGet("http://localhost:9090")
		assertStatusCode(b, status_code, http.StatusOK)
		assertForPrimaryResponse(b, broadcast_res)
		waitForSecondaryResponses(res_chan)
	}
}

func assertStatusCode(tb testing.TB, expected_status_code, actual_status_code int) {
	if actual_status_code != expected_status_code {
		tb.Errorf("Expected status code: %d. Actual status code: %d", expected_status_code, actual_status_code)
	}
}

func assertForPrimaryResponse(tb testing.TB, response_str string) {
	if primary_tag := readTag(response_str); primary_tag != PrimaryTag {
		tb.Errorf("Expected primary tag %s, Actual primary tag %s. Broadcast Response %s", PrimaryTag, primary_tag, response_str)
	}
}

func waitForSecondaryResponses(res_chan <-chan string) {
	timer := time.NewTimer(1 * time.Second)
	defer timer.Stop()
	for i := 1; i <= len(backendServers); {
		select {
		case <-timer.C:
			timer.Reset(time.Duration(i) * time.Second)
		case <-res_chan:
			i++
			// log.Printf("Response from backend server. Tag: %s. Response: %s\n", readTag(msg), msg)
		}
	}
}
