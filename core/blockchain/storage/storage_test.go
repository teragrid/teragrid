package storage

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	mrand "math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	amino "github.com/teragrid/dgrid/third_party/amino"

	"github.com/teragrid/dgrid/asura/example/counter"
	"github.com/teragrid/dgrid/asura/example/kvstore"
	asuraserver "github.com/teragrid/dgrid/asura/server"
	asura "github.com/teragrid/dgrid/asura/types"
	cfg "github.com/teragrid/dgrid/core/config"
	cmn "github.com/teragrid/dgrid/pkg/common"
	"github.com/teragrid/dgrid/pkg/log"
	"github.com/teragrid/dgrid/proxy"
	"github.com/teragrid/dgrid/core/types"
)

// A cleanupFunc cleans up any config / test files created for a particular
// test.
type cleanupFunc func()

func newStorageWithApp(cc proxy.ClientCreator) (*Storage, cleanupFunc) {
	return newStorageWithAppAndConfig(cc, cfg.ResetTestRoot("storage_test"))
}

func newStorageWithAppAndConfig(cc proxy.ClientCreator, config *cfg.Config) (*Storage, cleanupFunc) {
	appConnMem, _ := cc.NewABCIClient()
	appConnMem.SetLogger(log.TestingLogger().With("module", "asura-client", "connection", "storage"))
	err := appConnMem.Start()
	if err != nil {
		panic(err)
	}
	storage := NewStorage(config.Storage, appConnMem, 0)
	storage.SetLogger(log.TestingLogger())
	return storage, func() { os.RemoveAll(config.RootDir) }
}

func ensureNoFire(t *testing.T, ch <-chan struct{}, timeoutMS int) {
	timer := time.NewTimer(time.Duration(timeoutMS) * time.Millisecond)
	select {
	case <-ch:
		t.Fatal("Expected not to fire")
	case <-timer.C:
	}
}

func ensureFire(t *testing.T, ch <-chan struct{}, timeoutMS int) {
	timer := time.NewTimer(time.Duration(timeoutMS) * time.Millisecond)
	select {
	case <-ch:
	case <-timer.C:
		t.Fatal("Expected to fire")
	}
}

func checkTxs(t *testing.T, storage *Storage, count int, peerID uint16) types.Txs {
	txs := make(types.Txs, count)
	txInfo := TxInfo{PeerID: peerID}
	for i := 0; i < count; i++ {
		txBytes := make([]byte, 20)
		txs[i] = txBytes
		_, err := rand.Read(txBytes)
		if err != nil {
			t.Error(err)
		}
		if err := storage.CheckTxWithInfo(txBytes, nil, txInfo); err != nil {
			// Skip invalid txs.
			// TestStorageFilters will fail otherwise. It asserts a number of txs
			// returned.
			if IsPreCheckError(err) {
				continue
			}
			t.Fatalf("CheckTx failed: %v while checking #%d tx", err, i)
		}
	}
	return txs
}

func TestReapMaxBytesMaxGas(t *testing.T) {
	app := kvstore.NewKVStoreApplication()
	cc := proxy.NewLocalClientCreator(app)
	storage, cleanup := newStorageWithApp(cc)
	defer cleanup()

	// Ensure gas calculation behaves as expected
	checkTxs(t, storage, 1, UnknownPeerID)
	tx0 := storage.TxsFront().Value.(*storageTx)
	// assert that kv store has gas wanted = 1.
	require.Equal(t, app.CheckTx(tx0.tx).GasWanted, int64(1), "KVStore had a gas value neq to 1")
	require.Equal(t, tx0.gasWanted, int64(1), "transactions gas was set incorrectly")
	// ensure each tx is 20 bytes long
	require.Equal(t, len(tx0.tx), 20, "Tx is longer than 20 bytes")
	storage.Flush()

	// each table driven test creates numTxsToCreate txs with checkTx, and at the end clears all remaining txs.
	// each tx has 20 bytes + amino overhead = 21 bytes, 1 gas
	tests := []struct {
		numTxsToCreate int
		maxBytes       int64
		maxGas         int64
		expectedNumTxs int
	}{
		{20, -1, -1, 20},
		{20, -1, 0, 0},
		{20, -1, 10, 10},
		{20, -1, 30, 20},
		{20, 0, -1, 0},
		{20, 0, 10, 0},
		{20, 10, 10, 0},
		{20, 22, 10, 1},
		{20, 220, -1, 10},
		{20, 220, 5, 5},
		{20, 220, 10, 10},
		{20, 220, 15, 10},
		{20, 20000, -1, 20},
		{20, 20000, 5, 5},
		{20, 20000, 30, 20},
	}
	for tcIndex, tt := range tests {
		checkTxs(t, storage, tt.numTxsToCreate, UnknownPeerID)
		got := storage.ReapMaxBytesMaxGas(tt.maxBytes, tt.maxGas)
		assert.Equal(t, tt.expectedNumTxs, len(got), "Got %d txs, expected %d, tc #%d",
			len(got), tt.expectedNumTxs, tcIndex)
		storage.Flush()
	}
}

func TestStorageFilters(t *testing.T) {
	app := kvstore.NewKVStoreApplication()
	cc := proxy.NewLocalClientCreator(app)
	storage, cleanup := newStorageWithApp(cc)
	defer cleanup()
	emptyTxArr := []types.Tx{[]byte{}}

	nopPreFilter := func(tx types.Tx) error { return nil }
	nopPostFilter := func(tx types.Tx, res *asura.ResponseCheckTx) error { return nil }

	// each table driven test creates numTxsToCreate txs with checkTx, and at the end clears all remaining txs.
	// each tx has 20 bytes + amino overhead = 21 bytes, 1 gas
	tests := []struct {
		numTxsToCreate int
		preFilter      PreCheckFunc
		postFilter     PostCheckFunc
		expectedNumTxs int
	}{
		{10, nopPreFilter, nopPostFilter, 10},
		{10, PreCheckAminoMaxBytes(10), nopPostFilter, 0},
		{10, PreCheckAminoMaxBytes(20), nopPostFilter, 0},
		{10, PreCheckAminoMaxBytes(22), nopPostFilter, 10},
		{10, nopPreFilter, PostCheckMaxGas(-1), 10},
		{10, nopPreFilter, PostCheckMaxGas(0), 0},
		{10, nopPreFilter, PostCheckMaxGas(1), 10},
		{10, nopPreFilter, PostCheckMaxGas(3000), 10},
		{10, PreCheckAminoMaxBytes(10), PostCheckMaxGas(20), 0},
		{10, PreCheckAminoMaxBytes(30), PostCheckMaxGas(20), 10},
		{10, PreCheckAminoMaxBytes(22), PostCheckMaxGas(1), 10},
		{10, PreCheckAminoMaxBytes(22), PostCheckMaxGas(0), 0},
	}
	for tcIndex, tt := range tests {
		storage.Update(1, emptyTxArr, tt.preFilter, tt.postFilter)
		checkTxs(t, storage, tt.numTxsToCreate, UnknownPeerID)
		require.Equal(t, tt.expectedNumTxs, storage.Size(), "storage had the incorrect size, on test case %d", tcIndex)
		storage.Flush()
	}
}

func TestStorageUpdateAddsTxsToCache(t *testing.T) {
	app := kvstore.NewKVStoreApplication()
	cc := proxy.NewLocalClientCreator(app)
	storage, cleanup := newStorageWithApp(cc)
	defer cleanup()
	storage.Update(1, []types.Tx{[]byte{0x01}}, nil, nil)
	err := storage.CheckTx([]byte{0x01}, nil)
	if assert.Error(t, err) {
		assert.Equal(t, ErrTxInCache, err)
	}
}

func TestTxsAvailable(t *testing.T) {
	app := kvstore.NewKVStoreApplication()
	cc := proxy.NewLocalClientCreator(app)
	storage, cleanup := newStorageWithApp(cc)
	defer cleanup()
	storage.EnableTxsAvailable()

	timeoutMS := 500

	// with no txs, it shouldnt fire
	ensureNoFire(t, storage.TxsAvailable(), timeoutMS)

	// send a bunch of txs, it should only fire once
	txs := checkTxs(t, storage, 100, UnknownPeerID)
	ensureFire(t, storage.TxsAvailable(), timeoutMS)
	ensureNoFire(t, storage.TxsAvailable(), timeoutMS)

	// call update with half the txs.
	// it should fire once now for the new height
	// since there are still txs left
	committedTxs, txs := txs[:50], txs[50:]
	if err := storage.Update(1, committedTxs, nil, nil); err != nil {
		t.Error(err)
	}
	ensureFire(t, storage.TxsAvailable(), timeoutMS)
	ensureNoFire(t, storage.TxsAvailable(), timeoutMS)

	// send a bunch more txs. we already fired for this height so it shouldnt fire again
	moreTxs := checkTxs(t, storage, 50, UnknownPeerID)
	ensureNoFire(t, storage.TxsAvailable(), timeoutMS)

	// now call update with all the txs. it should not fire as there are no txs left
	committedTxs = append(txs, moreTxs...)
	if err := storage.Update(2, committedTxs, nil, nil); err != nil {
		t.Error(err)
	}
	ensureNoFire(t, storage.TxsAvailable(), timeoutMS)

	// send a bunch more txs, it should only fire once
	checkTxs(t, storage, 100, UnknownPeerID)
	ensureFire(t, storage.TxsAvailable(), timeoutMS)
	ensureNoFire(t, storage.TxsAvailable(), timeoutMS)
}

func TestSerialReap(t *testing.T) {
	app := counter.NewCounterApplication(true)
	app.SetOption(asura.RequestSetOption{Key: "serial", Value: "on"})
	cc := proxy.NewLocalClientCreator(app)

	storage, cleanup := newStorageWithApp(cc)
	defer cleanup()

	appConnCon, _ := cc.NewABCIClient()
	appConnCon.SetLogger(log.TestingLogger().With("module", "asura-client", "connection", "consensus"))
	err := appConnCon.Start()
	require.Nil(t, err)

	cacheMap := make(map[string]struct{})
	deliverTxsRange := func(start, end int) {
		// Deliver some txs.
		for i := start; i < end; i++ {

			// This will succeed
			txBytes := make([]byte, 8)
			binary.BigEndian.PutUint64(txBytes, uint64(i))
			err := storage.CheckTx(txBytes, nil)
			_, cached := cacheMap[string(txBytes)]
			if cached {
				require.NotNil(t, err, "expected error for cached tx")
			} else {
				require.Nil(t, err, "expected no err for uncached tx")
			}
			cacheMap[string(txBytes)] = struct{}{}

			// Duplicates are cached and should return error
			err = storage.CheckTx(txBytes, nil)
			require.NotNil(t, err, "Expected error after CheckTx on duplicated tx")
		}
	}

	reapCheck := func(exp int) {
		txs := storage.ReapMaxBytesMaxGas(-1, -1)
		require.Equal(t, len(txs), exp, fmt.Sprintf("Expected to reap %v txs but got %v", exp, len(txs)))
	}

	updateRange := func(start, end int) {
		txs := make([]types.Tx, 0)
		for i := start; i < end; i++ {
			txBytes := make([]byte, 8)
			binary.BigEndian.PutUint64(txBytes, uint64(i))
			txs = append(txs, txBytes)
		}
		if err := storage.Update(0, txs, nil, nil); err != nil {
			t.Error(err)
		}
	}

	commitRange := func(start, end int) {
		// Deliver some txs.
		for i := start; i < end; i++ {
			txBytes := make([]byte, 8)
			binary.BigEndian.PutUint64(txBytes, uint64(i))
			res, err := appConnCon.DeliverTxSync(txBytes)
			if err != nil {
				t.Errorf("Client error committing tx: %v", err)
			}
			if res.IsErr() {
				t.Errorf("Error committing tx. Code:%v result:%X log:%v",
					res.Code, res.Data, res.Log)
			}
		}
		res, err := appConnCon.CommitSync()
		if err != nil {
			t.Errorf("Client error committing: %v", err)
		}
		if len(res.Data) != 8 {
			t.Errorf("Error committing. Hash:%X", res.Data)
		}
	}

	//----------------------------------------

	// Deliver some txs.
	deliverTxsRange(0, 100)

	// Reap the txs.
	reapCheck(100)

	// Reap again.  We should get the same amount
	reapCheck(100)

	// Deliver 0 to 999, we should reap 900 new txs
	// because 100 were already counted.
	deliverTxsRange(0, 1000)

	// Reap the txs.
	reapCheck(1000)

	// Reap again.  We should get the same amount
	reapCheck(1000)

	// Commit from the conensus AppConn
	commitRange(0, 500)
	updateRange(0, 500)

	// We should have 500 left.
	reapCheck(500)

	// Deliver 100 invalid txs and 100 valid txs
	deliverTxsRange(900, 1100)

	// We should have 600 now.
	reapCheck(600)
}

func TestStorageCloseWAL(t *testing.T) {
	// 1. Create the temporary directory for storage and WAL testing.
	rootDir, err := ioutil.TempDir("", "storage-test")
	require.Nil(t, err, "expecting successful tmpdir creation")
	defer os.RemoveAll(rootDir)

	// 2. Ensure that it doesn't contain any elements -- Sanity check
	m1, err := filepath.Glob(filepath.Join(rootDir, "*"))
	require.Nil(t, err, "successful globbing expected")
	require.Equal(t, 0, len(m1), "no matches yet")

	// 3. Create the storage
	wcfg := cfg.DefaultStorageConfig()
	wcfg.RootDir = rootDir
	defer os.RemoveAll(wcfg.RootDir)
	app := kvstore.NewKVStoreApplication()
	cc := proxy.NewLocalClientCreator(app)
	appConnMem, _ := cc.NewABCIClient()
	storage := NewStorage(wcfg, appConnMem, 10)
	storage.InitWAL()

	// 4. Ensure that the directory contains the WAL file
	m2, err := filepath.Glob(filepath.Join(rootDir, "*"))
	require.Nil(t, err, "successful globbing expected")
	require.Equal(t, 1, len(m2), "expecting the wal match in")

	// 5. Write some contents to the WAL
	storage.CheckTx(types.Tx([]byte("foo")), nil)
	walFilepath := storage.wal.Path
	sum1 := checksumFile(walFilepath, t)

	// 6. Sanity check to ensure that the written TX matches the expectation.
	require.Equal(t, sum1, checksumIt([]byte("foo\n")), "foo with a newline should be written")

	// 7. Invoke CloseWAL() and ensure it discards the
	// WAL thus any other write won't go through.
	storage.CloseWAL()
	storage.CheckTx(types.Tx([]byte("bar")), nil)
	sum2 := checksumFile(walFilepath, t)
	require.Equal(t, sum1, sum2, "expected no change to the WAL after invoking CloseWAL() since it was discarded")

	// 8. Sanity check to ensure that the WAL file still exists
	m3, err := filepath.Glob(filepath.Join(rootDir, "*"))
	require.Nil(t, err, "successful globbing expected")
	require.Equal(t, 1, len(m3), "expecting the wal match in")
}

// Size of the amino encoded TxMessage is the length of the
// encoded byte array, plus 1 for the struct field, plus 4
// for the amino prefix.
func txMessageSize(tx types.Tx) int {
	return amino.ByteSliceSize(tx) + 1 + 4
}

func TestStorageMaxMsgSize(t *testing.T) {
	app := kvstore.NewKVStoreApplication()
	cc := proxy.NewLocalClientCreator(app)
	mempl, cleanup := newStorageWithApp(cc)
	defer cleanup()

	testCases := []struct {
		len int
		err bool
	}{
		// check small txs. no error
		{10, false},
		{1000, false},
		{1000000, false},

		// check around maxTxSize
		// changes from no error to error
		{maxTxSize - 2, false},
		{maxTxSize - 1, false},
		{maxTxSize, false},
		{maxTxSize + 1, true},
		{maxTxSize + 2, true},

		// check around maxMsgSize. all error
		{maxMsgSize - 1, true},
		{maxMsgSize, true},
		{maxMsgSize + 1, true},
	}

	for i, testCase := range testCases {
		caseString := fmt.Sprintf("case %d, len %d", i, testCase.len)

		tx := cmn.RandBytes(testCase.len)
		err := mempl.CheckTx(tx, nil)
		msg := &TxMessage{tx}
		encoded := cdc.MustMarshalBinaryBare(msg)
		require.Equal(t, len(encoded), txMessageSize(tx), caseString)
		if !testCase.err {
			require.True(t, len(encoded) <= maxMsgSize, caseString)
			require.NoError(t, err, caseString)
		} else {
			require.True(t, len(encoded) > maxMsgSize, caseString)
			require.Equal(t, err, ErrTxTooLarge, caseString)
		}
	}

}

func TestStorageTxsBytes(t *testing.T) {
	app := kvstore.NewKVStoreApplication()
	cc := proxy.NewLocalClientCreator(app)
	config := cfg.ResetTestRoot("storage_test")
	config.Storage.MaxTxsBytes = 10
	storage, cleanup := newStorageWithAppAndConfig(cc, config)
	defer cleanup()

	// 1. zero by default
	assert.EqualValues(t, 0, storage.TxsBytes())

	// 2. len(tx) after CheckTx
	err := storage.CheckTx([]byte{0x01}, nil)
	require.NoError(t, err)
	assert.EqualValues(t, 1, storage.TxsBytes())

	// 3. zero again after tx is removed by Update
	storage.Update(1, []types.Tx{[]byte{0x01}}, nil, nil)
	assert.EqualValues(t, 0, storage.TxsBytes())

	// 4. zero after Flush
	err = storage.CheckTx([]byte{0x02, 0x03}, nil)
	require.NoError(t, err)
	assert.EqualValues(t, 2, storage.TxsBytes())

	storage.Flush()
	assert.EqualValues(t, 0, storage.TxsBytes())

	// 5. ErrStorageIsFull is returned when/if MaxTxsBytes limit is reached.
	err = storage.CheckTx([]byte{0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04}, nil)
	require.NoError(t, err)
	err = storage.CheckTx([]byte{0x05}, nil)
	if assert.Error(t, err) {
		assert.IsType(t, ErrStorageIsFull{}, err)
	}

	// 6. zero after tx is rechecked and removed due to not being valid anymore
	app2 := counter.NewCounterApplication(true)
	cc = proxy.NewLocalClientCreator(app2)
	storage, cleanup = newStorageWithApp(cc)
	defer cleanup()

	txBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(txBytes, uint64(0))

	err = storage.CheckTx(txBytes, nil)
	require.NoError(t, err)
	assert.EqualValues(t, 8, storage.TxsBytes())

	appConnCon, _ := cc.NewABCIClient()
	appConnCon.SetLogger(log.TestingLogger().With("module", "asura-client", "connection", "consensus"))
	err = appConnCon.Start()
	require.Nil(t, err)
	defer appConnCon.Stop()
	res, err := appConnCon.DeliverTxSync(txBytes)
	require.NoError(t, err)
	require.EqualValues(t, 0, res.Code)
	res2, err := appConnCon.CommitSync()
	require.NoError(t, err)
	require.NotEmpty(t, res2.Data)

	// Pretend like we committed nothing so txBytes gets rechecked and removed.
	storage.Update(1, []types.Tx{}, nil, nil)
	assert.EqualValues(t, 0, storage.TxsBytes())
}

// This will non-deterministically catch some concurrency failures like
// https://github.com/teragrid/dgrid/core/issues/3509
// TODO: all of the tests should probably also run using the remote proxy app
// since otherwise we're not actually testing the concurrency of the storage here!
func TestStorageRemoteAppConcurrency(t *testing.T) {
	sockPath := fmt.Sprintf("unix:///tmp/echo_%v.sock", cmn.RandStr(6))
	app := kvstore.NewKVStoreApplication()
	cc, server := newRemoteApp(t, sockPath, app)
	defer server.Stop()
	config := cfg.ResetTestRoot("storage_test")
	storage, cleanup := newStorageWithAppAndConfig(cc, config)
	defer cleanup()

	// generate small number of txs
	nTxs := 10
	txLen := 200
	txs := make([]types.Tx, nTxs)
	for i := 0; i < nTxs; i++ {
		txs[i] = cmn.RandBytes(txLen)
	}

	// simulate a group of peers sending them over and over
	N := config.Storage.Size
	maxPeers := 5
	for i := 0; i < N; i++ {
		peerID := mrand.Intn(maxPeers)
		txNum := mrand.Intn(nTxs)
		tx := txs[int(txNum)]

		// this will err with ErrTxInCache many times ...
		storage.CheckTxWithInfo(tx, nil, TxInfo{PeerID: uint16(peerID)})
	}
	err := storage.FlushAppConn()
	require.NoError(t, err)
}

// caller must close server
func newRemoteApp(t *testing.T, addr string, app asura.Application) (clientCreator proxy.ClientCreator, server cmn.Service) {
	clientCreator = proxy.NewRemoteClientCreator(addr, "socket", true)

	// Start server
	server = asuraserver.NewSocketServer(addr, app)
	server.SetLogger(log.TestingLogger().With("module", "asura-server"))
	if err := server.Start(); err != nil {
		t.Fatalf("Error starting socket server: %v", err.Error())
	}
	return clientCreator, server
}
func checksumIt(data []byte) string {
	h := sha256.New()
	h.Write(data)
	return fmt.Sprintf("%x", h.Sum(nil))
}

func checksumFile(p string, t *testing.T) string {
	data, err := ioutil.ReadFile(p)
	require.Nil(t, err, "expecting successful read of %q", p)
	return checksumIt(data)
}