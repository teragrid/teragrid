package proxy

import (
	"fmt"
	"strings"
	"testing"

	asura "github.com/teragrid/dgrid/asura/client"
	"github.com/teragrid/dgrid/asura/example/kvstore"
	"github.com/teragrid/dgrid/asura/server"
	"github.com/teragrid/dgrid/asura/types"
	cmn "github.com/teragrid/dgrid/pkg/common"
	"github.com/teragrid/dgrid/pkg/log"
)

//----------------------------------------

type AppConnTest interface {
	EchoAsync(string) *asura.ReqRes
	FlushSync() error
	InfoSync(types.RequestInfo) (*types.ResponseInfo, error)
}

type appConnTest struct {
	appConn asura.Client
}

func NewAppConnTest(appConn asura.Client) AppConnTest {
	return &appConnTest{appConn}
}

func (app *appConnTest) EchoAsync(msg string) *asura.ReqRes {
	return app.appConn.EchoAsync(msg)
}

func (app *appConnTest) FlushSync() error {
	return app.appConn.FlushSync()
}

func (app *appConnTest) InfoSync(req types.RequestInfo) (*types.ResponseInfo, error) {
	return app.appConn.InfoSync(req)
}

//----------------------------------------

var SOCKET = "socket"

func TestEcho(t *testing.T) {
	sockPath := fmt.Sprintf("unix:///tmp/echo_%v.sock", cmn.RandStr(6))
	clientCreator := NewRemoteClientCreator(sockPath, SOCKET, true)

	// Start server
	s := server.NewSocketServer(sockPath, kvstore.NewKVStoreApplication())
	s.SetLogger(log.TestingLogger().With("module", "asura-server"))
	if err := s.Start(); err != nil {
		t.Fatalf("Error starting socket server: %v", err.Error())
	}
	defer s.Stop()

	// Start client
	cli, err := clientCreator.NewAsuraClient()
	if err != nil {
		t.Fatalf("Error creating Asura client: %v", err.Error())
	}
	cli.SetLogger(log.TestingLogger().With("module", "asura-client"))
	if err := cli.Start(); err != nil {
		t.Fatalf("Error starting Asura client: %v", err.Error())
	}

	proxy := NewAppConnTest(cli)
	t.Log("Connected")

	for i := 0; i < 1000; i++ {
		proxy.EchoAsync(fmt.Sprintf("echo-%v", i))
	}
	if err := proxy.FlushSync(); err != nil {
		t.Error(err)
	}
}

func BenchmarkEcho(b *testing.B) {
	b.StopTimer() // Initialize
	sockPath := fmt.Sprintf("unix:///tmp/echo_%v.sock", cmn.RandStr(6))
	clientCreator := NewRemoteClientCreator(sockPath, SOCKET, true)

	// Start server
	s := server.NewSocketServer(sockPath, kvstore.NewKVStoreApplication())
	s.SetLogger(log.TestingLogger().With("module", "asura-server"))
	if err := s.Start(); err != nil {
		b.Fatalf("Error starting socket server: %v", err.Error())
	}
	defer s.Stop()

	// Start client
	cli, err := clientCreator.NewAsuraClient()
	if err != nil {
		b.Fatalf("Error creating Asura client: %v", err.Error())
	}
	cli.SetLogger(log.TestingLogger().With("module", "asura-client"))
	if err := cli.Start(); err != nil {
		b.Fatalf("Error starting Asura client: %v", err.Error())
	}

	proxy := NewAppConnTest(cli)
	b.Log("Connected")
	echoString := strings.Repeat(" ", 200)
	b.StartTimer() // Start benchmarking tests

	for i := 0; i < b.N; i++ {
		proxy.EchoAsync(echoString)
	}
	if err := proxy.FlushSync(); err != nil {
		b.Error(err)
	}

	b.StopTimer()
	// info := proxy.InfoSync(types.RequestInfo{""})
	//b.Log("N: ", b.N, info)
}

func TestInfo(t *testing.T) {
	sockPath := fmt.Sprintf("unix:///tmp/echo_%v.sock", cmn.RandStr(6))
	clientCreator := NewRemoteClientCreator(sockPath, SOCKET, true)

	// Start server
	s := server.NewSocketServer(sockPath, kvstore.NewKVStoreApplication())
	s.SetLogger(log.TestingLogger().With("module", "asura-server"))
	if err := s.Start(); err != nil {
		t.Fatalf("Error starting socket server: %v", err.Error())
	}
	defer s.Stop()

	// Start client
	cli, err := clientCreator.NewAsuraClient()
	if err != nil {
		t.Fatalf("Error creating Asura client: %v", err.Error())
	}
	cli.SetLogger(log.TestingLogger().With("module", "asura-client"))
	if err := cli.Start(); err != nil {
		t.Fatalf("Error starting Asura client: %v", err.Error())
	}

	proxy := NewAppConnTest(cli)
	t.Log("Connected")

	resInfo, err := proxy.InfoSync(RequestInfo)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if string(resInfo.Data) != "{\"size\":0}" {
		t.Error("Expected ResponseInfo with one element '{\"size\":0}' but got something else")
	}
}
