package api

// #include <stdlib.h>
// #include "bindings.h"
import "C"

import (
	"fmt"
	"runtime"
	"syscall"

	"github.com/line/wasmvm/types"
)

// Value types
type (
	cint   = C.int
	cbool  = C.bool
	cusize = C.size_t
	cu8    = C.uint8_t
	cu32   = C.uint32_t
	cu64   = C.uint64_t
	ci8    = C.int8_t
	ci32   = C.int32_t
	ci64   = C.int64_t
)

// Pointers
type cu8_ptr = *C.uint8_t

type Cache struct {
	ptr *C.cache_t
}

type Env = types.Env

type Querier = types.Querier

func InitCache(dataDir string, supportedFeatures string, cacheSize uint32, instanceMemoryLimit uint32) (Cache, error) {
	dataDirBytes := []byte(dataDir)
	supportedFeaturesBytes := []byte(supportedFeatures)

	d := makeView(dataDirBytes)
	defer runtime.KeepAlive(dataDirBytes)
	f := makeView(supportedFeaturesBytes)
	defer runtime.KeepAlive(supportedFeaturesBytes)

	errmsg := newUnmanagedVector(nil)

	ptr, err := C.init_cache(d, f, cu32(cacheSize), cu32(instanceMemoryLimit), &errmsg)
	if err != nil {
		return Cache{}, errorWithMessage(err, errmsg)
	}
	return Cache{ptr: ptr}, nil
}

func ReleaseCache(cache Cache) {
	C.release_cache(cache.ptr)
}

func Create(cache Cache, wasm []byte) ([]byte, error) {
	w := makeView(wasm)
	defer runtime.KeepAlive(wasm)
	errmsg := newUnmanagedVector(nil)
	checksum, err := C.save_wasm(cache.ptr, w, &errmsg)
	if err != nil {
		return nil, errorWithMessage(err, errmsg)
	}
	return copyAndDestroyUnmanagedVector(checksum), nil
}

func GetCode(cache Cache, checksum []byte) ([]byte, error) {
	cs := makeView(checksum)
	defer runtime.KeepAlive(checksum)
	errmsg := newUnmanagedVector(nil)
	wasm, err := C.load_wasm(cache.ptr, cs, &errmsg)
	if err != nil {
		return nil, errorWithMessage(err, errmsg)
	}
	return copyAndDestroyUnmanagedVector(wasm), nil
}

func Pin(cache Cache, checksum []byte) error {
	cs := makeView(checksum)
	defer runtime.KeepAlive(checksum)
	errmsg := newUnmanagedVector(nil)
	_, err := C.pin(cache.ptr, cs, &errmsg)
	if err != nil {
		return errorWithMessage(err, errmsg)
	}
	return nil
}

func Unpin(cache Cache, checksum []byte) error {
	cs := makeView(checksum)
	defer runtime.KeepAlive(checksum)
	errmsg := newUnmanagedVector(nil)
	_, err := C.unpin(cache.ptr, cs, &errmsg)
	if err != nil {
		return errorWithMessage(err, errmsg)
	}
	return nil
}

func AnalyzeCode(cache Cache, checksum []byte) (*types.AnalysisReport, error) {
	cs := makeView(checksum)
	defer runtime.KeepAlive(checksum)
	errmsg := newUnmanagedVector(nil)
	report, err := C.analyze_code(cache.ptr, cs, &errmsg)
	if err != nil {
		return nil, errorWithMessage(err, errmsg)
	}
	requiredCapabilities := string(copyAndDestroyUnmanagedVector(report.required_capabilities))
	res := types.AnalysisReport{
		HasIBCEntryPoints:    bool(report.has_ibc_entry_points),
		RequiredFeatures:     requiredCapabilities,
		RequiredCapabilities: requiredCapabilities,
	}
	return &res, nil
}

func GetMetrics(cache Cache) (*types.Metrics, error) {
	errmsg := newUnmanagedVector(nil)
	metrics, err := C.get_metrics(cache.ptr, &errmsg)
	if err != nil {
		return nil, errorWithMessage(err, errmsg)
	}

	return &types.Metrics{
		HitsPinnedMemoryCache:     uint32(metrics.hits_pinned_memory_cache),
		HitsMemoryCache:           uint32(metrics.hits_memory_cache),
		HitsFsCache:               uint32(metrics.hits_fs_cache),
		Misses:                    uint32(metrics.misses),
		ElementsPinnedMemoryCache: uint64(metrics.elements_pinned_memory_cache),
		ElementsMemoryCache:       uint64(metrics.elements_memory_cache),
		SizePinnedMemoryCache:     uint64(metrics.size_pinned_memory_cache),
		SizeMemoryCache:           uint64(metrics.size_memory_cache),
	}, nil
}

func Instantiate(
	cache Cache,
	checksum []byte,
	env []byte,
	info []byte,
	msg []byte,
	gasMeter *GasMeter,
	store KVStore,
	api *GoAPI,
	querier *Querier,
	gasLimit uint64,
	printDebug bool,
) ([]byte, []byte, []byte, uint64, error) {
	cs := makeView(checksum)
	defer runtime.KeepAlive(checksum)
	e := makeView(env)
	defer runtime.KeepAlive(env)
	i := makeView(info)
	defer runtime.KeepAlive(info)
	m := makeView(msg)
	defer runtime.KeepAlive(msg)

	callID := startCall()
	defer endCall(callID)

	dbState := buildDBState(store, callID)
	db := buildDB(&dbState, gasMeter)
	a := buildAPI(api)
	q := buildQuerier(querier)
	var gasUsed cu64
	errmsg := newUnmanagedVector(nil)
	events := newUnmanagedVector(nil)
	attributes := newUnmanagedVector(nil)

	res, err := C.instantiate(cache.ptr, cs, e, i, m, db, a, q, cu64(gasLimit), cbool(printDebug), &gasUsed, &events, &attributes, &errmsg)
	if err != nil && err.(syscall.Errno) != C.ErrnoValue_Success {
		// Depending on the nature of the error, `gasUsed` will either have a meaningful value, or just 0.
		return nil, nil, nil, uint64(gasUsed), errorWithMessage(err, errmsg)
	}
	return copyAndDestroyUnmanagedVector(res), copyAndDestroyUnmanagedVector(events), copyAndDestroyUnmanagedVector(attributes), uint64(gasUsed), nil
}

func Execute(
	cache Cache,
	checksum []byte,
	env []byte,
	info []byte,
	msg []byte,
	gasMeter *GasMeter,
	store KVStore,
	api *GoAPI,
	querier *Querier,
	gasLimit uint64,
	printDebug bool,
) ([]byte, []byte, []byte, uint64, error) {
	cs := makeView(checksum)
	defer runtime.KeepAlive(checksum)
	e := makeView(env)
	defer runtime.KeepAlive(env)
	i := makeView(info)
	defer runtime.KeepAlive(info)
	m := makeView(msg)
	defer runtime.KeepAlive(msg)

	callID := startCall()
	defer endCall(callID)

	dbState := buildDBState(store, callID)
	db := buildDB(&dbState, gasMeter)
	a := buildAPI(api)
	q := buildQuerier(querier)
	var gasUsed cu64
	errmsg := newUnmanagedVector(nil)
	events := newUnmanagedVector(nil)
	attributes := newUnmanagedVector(nil)

	res, err := C.execute(cache.ptr, cs, e, i, m, db, a, q, cu64(gasLimit), cbool(printDebug), &gasUsed, &events, &attributes, &errmsg)
	if err != nil && err.(syscall.Errno) != C.ErrnoValue_Success {
		// Depending on the nature of the error, `gasUsed` will either have a meaningful value, or just 0.
		return nil, nil, nil, uint64(gasUsed), errorWithMessage(err, errmsg)
	}
	return copyAndDestroyUnmanagedVector(res), copyAndDestroyUnmanagedVector(events), copyAndDestroyUnmanagedVector(attributes), uint64(gasUsed), nil
}

func Migrate(
	cache Cache,
	checksum []byte,
	env []byte,
	msg []byte,
	gasMeter *GasMeter,
	store KVStore,
	api *GoAPI,
	querier *Querier,
	gasLimit uint64,
	printDebug bool,
) ([]byte, []byte, []byte, uint64, error) {
	cs := makeView(checksum)
	defer runtime.KeepAlive(checksum)
	e := makeView(env)
	defer runtime.KeepAlive(env)
	m := makeView(msg)
	defer runtime.KeepAlive(msg)

	callID := startCall()
	defer endCall(callID)

	dbState := buildDBState(store, callID)
	db := buildDB(&dbState, gasMeter)
	a := buildAPI(api)
	q := buildQuerier(querier)
	var gasUsed cu64
	errmsg := newUnmanagedVector(nil)
	events := newUnmanagedVector(nil)
	attributes := newUnmanagedVector(nil)

	res, err := C.migrate(cache.ptr, cs, e, m, db, a, q, cu64(gasLimit), cbool(printDebug), &gasUsed, &events, &attributes, &errmsg)
	if err != nil && err.(syscall.Errno) != C.ErrnoValue_Success {
		// Depending on the nature of the error, `gasUsed` will either have a meaningful value, or just 0.
		return nil, nil, nil, uint64(gasUsed), errorWithMessage(err, errmsg)
	}
	return copyAndDestroyUnmanagedVector(res), copyAndDestroyUnmanagedVector(events), copyAndDestroyUnmanagedVector(attributes), uint64(gasUsed), nil
}

func Sudo(
	cache Cache,
	checksum []byte,
	env []byte,
	msg []byte,
	gasMeter *GasMeter,
	store KVStore,
	api *GoAPI,
	querier *Querier,
	gasLimit uint64,
	printDebug bool,
) ([]byte, []byte, []byte, uint64, error) {
	cs := makeView(checksum)
	defer runtime.KeepAlive(checksum)
	e := makeView(env)
	defer runtime.KeepAlive(env)
	m := makeView(msg)
	defer runtime.KeepAlive(msg)

	callID := startCall()
	defer endCall(callID)

	dbState := buildDBState(store, callID)
	db := buildDB(&dbState, gasMeter)
	a := buildAPI(api)
	q := buildQuerier(querier)
	var gasUsed cu64
	errmsg := newUnmanagedVector(nil)
	events := newUnmanagedVector(nil)
	attributes := newUnmanagedVector(nil)

	res, err := C.sudo(cache.ptr, cs, e, m, db, a, q, cu64(gasLimit), cbool(printDebug), &gasUsed, &events, &attributes, &errmsg)
	if err != nil && err.(syscall.Errno) != C.ErrnoValue_Success {
		// Depending on the nature of the error, `gasUsed` will either have a meaningful value, or just 0.
		return nil, nil, nil, uint64(gasUsed), errorWithMessage(err, errmsg)
	}
	return copyAndDestroyUnmanagedVector(res), copyAndDestroyUnmanagedVector(events), copyAndDestroyUnmanagedVector(attributes), uint64(gasUsed), nil
}

func Reply(
	cache Cache,
	checksum []byte,
	env []byte,
	reply []byte,
	gasMeter *GasMeter,
	store KVStore,
	api *GoAPI,
	querier *Querier,
	gasLimit uint64,
	printDebug bool,
) ([]byte, []byte, []byte, uint64, error) {
	cs := makeView(checksum)
	defer runtime.KeepAlive(checksum)
	e := makeView(env)
	defer runtime.KeepAlive(env)
	r := makeView(reply)
	defer runtime.KeepAlive(reply)

	callID := startCall()
	defer endCall(callID)

	dbState := buildDBState(store, callID)
	db := buildDB(&dbState, gasMeter)
	a := buildAPI(api)
	q := buildQuerier(querier)
	var gasUsed cu64
	errmsg := newUnmanagedVector(nil)
	events := newUnmanagedVector(nil)
	attributes := newUnmanagedVector(nil)

	res, err := C.reply(cache.ptr, cs, e, r, db, a, q, cu64(gasLimit), cbool(printDebug), &gasUsed, &events, &attributes, &errmsg)
	if err != nil && err.(syscall.Errno) != C.ErrnoValue_Success {
		// Depending on the nature of the error, `gasUsed` will either have a meaningful value, or just 0.
		return nil, nil, nil, uint64(gasUsed), errorWithMessage(err, errmsg)
	}
	return copyAndDestroyUnmanagedVector(res), copyAndDestroyUnmanagedVector(events), copyAndDestroyUnmanagedVector(attributes), uint64(gasUsed), nil
}

func Query(
	cache Cache,
	checksum []byte,
	env []byte,
	msg []byte,
	gasMeter *GasMeter,
	store KVStore,
	api *GoAPI,
	querier *Querier,
	gasLimit uint64,
	printDebug bool,
) ([]byte, uint64, error) {
	cs := makeView(checksum)
	defer runtime.KeepAlive(checksum)
	e := makeView(env)
	defer runtime.KeepAlive(env)
	m := makeView(msg)
	defer runtime.KeepAlive(msg)

	callID := startCall()
	defer endCall(callID)

	dbState := buildDBState(store, callID)
	db := buildDB(&dbState, gasMeter)
	a := buildAPI(api)
	q := buildQuerier(querier)
	var gasUsed cu64
	errmsg := newUnmanagedVector(nil)

	res, err := C.query(cache.ptr, cs, e, m, db, a, q, cu64(gasLimit), cbool(printDebug), &gasUsed, &errmsg)
	if err != nil && err.(syscall.Errno) != C.ErrnoValue_Success {
		// Depending on the nature of the error, `gasUsed` will either have a meaningful value, or just 0.
		return nil, uint64(gasUsed), errorWithMessage(err, errmsg)
	}
	return copyAndDestroyUnmanagedVector(res), uint64(gasUsed), nil
}

func IBCChannelOpen(
	cache Cache,
	checksum []byte,
	env []byte,
	msg []byte,
	gasMeter *GasMeter,
	store KVStore,
	api *GoAPI,
	querier *Querier,
	gasLimit uint64,
	printDebug bool,
) ([]byte, uint64, error) {
	cs := makeView(checksum)
	defer runtime.KeepAlive(checksum)
	e := makeView(env)
	defer runtime.KeepAlive(env)
	m := makeView(msg)
	defer runtime.KeepAlive(msg)

	callID := startCall()
	defer endCall(callID)

	dbState := buildDBState(store, callID)
	db := buildDB(&dbState, gasMeter)
	a := buildAPI(api)
	q := buildQuerier(querier)
	var gasUsed cu64
	errmsg := newUnmanagedVector(nil)

	res, err := C.ibc_channel_open(cache.ptr, cs, e, m, db, a, q, cu64(gasLimit), cbool(printDebug), &gasUsed, &errmsg)
	if err != nil && err.(syscall.Errno) != C.ErrnoValue_Success {
		// Depending on the nature of the error, `gasUsed` will either have a meaningful value, or just 0.
		return nil, uint64(gasUsed), errorWithMessage(err, errmsg)
	}
	return copyAndDestroyUnmanagedVector(res), uint64(gasUsed), nil
}

func IBCChannelConnect(
	cache Cache,
	checksum []byte,
	env []byte,
	msg []byte,
	gasMeter *GasMeter,
	store KVStore,
	api *GoAPI,
	querier *Querier,
	gasLimit uint64,
	printDebug bool,
) ([]byte, []byte, []byte, uint64, error) {
	cs := makeView(checksum)
	defer runtime.KeepAlive(checksum)
	e := makeView(env)
	defer runtime.KeepAlive(env)
	m := makeView(msg)
	defer runtime.KeepAlive(msg)

	callID := startCall()
	defer endCall(callID)

	dbState := buildDBState(store, callID)
	db := buildDB(&dbState, gasMeter)
	a := buildAPI(api)
	q := buildQuerier(querier)
	var gasUsed cu64
	errmsg := newUnmanagedVector(nil)
	events := newUnmanagedVector(nil)
	attributes := newUnmanagedVector(nil)

	res, err := C.ibc_channel_connect(cache.ptr, cs, e, m, db, a, q, cu64(gasLimit), cbool(printDebug), &gasUsed, &events, &attributes, &errmsg)
	if err != nil && err.(syscall.Errno) != C.ErrnoValue_Success {
		// Depending on the nature of the error, `gasUsed` will either have a meaningful value, or just 0.
		return nil, nil, nil, uint64(gasUsed), errorWithMessage(err, errmsg)
	}
	return copyAndDestroyUnmanagedVector(res), copyAndDestroyUnmanagedVector(events), copyAndDestroyUnmanagedVector(attributes), uint64(gasUsed), nil
}

func IBCChannelClose(
	cache Cache,
	checksum []byte,
	env []byte,
	msg []byte,
	gasMeter *GasMeter,
	store KVStore,
	api *GoAPI,
	querier *Querier,
	gasLimit uint64,
	printDebug bool,
) ([]byte, []byte, []byte, uint64, error) {
	cs := makeView(checksum)
	defer runtime.KeepAlive(checksum)
	e := makeView(env)
	defer runtime.KeepAlive(env)
	m := makeView(msg)
	defer runtime.KeepAlive(msg)

	callID := startCall()
	defer endCall(callID)

	dbState := buildDBState(store, callID)
	db := buildDB(&dbState, gasMeter)
	a := buildAPI(api)
	q := buildQuerier(querier)
	var gasUsed cu64
	errmsg := newUnmanagedVector(nil)
	events := newUnmanagedVector(nil)
	attributes := newUnmanagedVector(nil)

	res, err := C.ibc_channel_close(cache.ptr, cs, e, m, db, a, q, cu64(gasLimit), cbool(printDebug), &gasUsed, &events, &attributes, &errmsg)
	if err != nil && err.(syscall.Errno) != C.ErrnoValue_Success {
		// Depending on the nature of the error, `gasUsed` will either have a meaningful value, or just 0.
		return nil, nil, nil, uint64(gasUsed), errorWithMessage(err, errmsg)
	}
	return copyAndDestroyUnmanagedVector(res), copyAndDestroyUnmanagedVector(events), copyAndDestroyUnmanagedVector(attributes), uint64(gasUsed), nil
}

func IBCPacketReceive(
	cache Cache,
	checksum []byte,
	env []byte,
	packet []byte,
	gasMeter *GasMeter,
	store KVStore,
	api *GoAPI,
	querier *Querier,
	gasLimit uint64,
	printDebug bool,
) ([]byte, []byte, []byte, uint64, error) {
	cs := makeView(checksum)
	defer runtime.KeepAlive(checksum)
	e := makeView(env)
	defer runtime.KeepAlive(env)
	pa := makeView(packet)
	defer runtime.KeepAlive(packet)

	callID := startCall()
	defer endCall(callID)

	dbState := buildDBState(store, callID)
	db := buildDB(&dbState, gasMeter)
	a := buildAPI(api)
	q := buildQuerier(querier)
	var gasUsed cu64
	errmsg := newUnmanagedVector(nil)
	events := newUnmanagedVector(nil)
	attributes := newUnmanagedVector(nil)

	res, err := C.ibc_packet_receive(cache.ptr, cs, e, pa, db, a, q, cu64(gasLimit), cbool(printDebug), &gasUsed, &events, &attributes, &errmsg)
	if err != nil && err.(syscall.Errno) != C.ErrnoValue_Success {
		// Depending on the nature of the error, `gasUsed` will either have a meaningful value, or just 0.
		return nil, nil, nil, uint64(gasUsed), errorWithMessage(err, errmsg)
	}
	return copyAndDestroyUnmanagedVector(res), copyAndDestroyUnmanagedVector(events), copyAndDestroyUnmanagedVector(attributes), uint64(gasUsed), nil
}

func IBCPacketAck(
	cache Cache,
	checksum []byte,
	env []byte,
	ack []byte,
	gasMeter *GasMeter,
	store KVStore,
	api *GoAPI,
	querier *Querier,
	gasLimit uint64,
	printDebug bool,
) ([]byte, []byte, []byte, uint64, error) {
	cs := makeView(checksum)
	defer runtime.KeepAlive(checksum)
	e := makeView(env)
	defer runtime.KeepAlive(env)
	ac := makeView(ack)
	defer runtime.KeepAlive(ack)

	callID := startCall()
	defer endCall(callID)

	dbState := buildDBState(store, callID)
	db := buildDB(&dbState, gasMeter)
	a := buildAPI(api)
	q := buildQuerier(querier)
	var gasUsed cu64
	errmsg := newUnmanagedVector(nil)
	events := newUnmanagedVector(nil)
	attributes := newUnmanagedVector(nil)

	res, err := C.ibc_packet_ack(cache.ptr, cs, e, ac, db, a, q, cu64(gasLimit), cbool(printDebug), &gasUsed, &events, &attributes, &errmsg)
	if err != nil && err.(syscall.Errno) != C.ErrnoValue_Success {
		// Depending on the nature of the error, `gasUsed` will either have a meaningful value, or just 0.
		return nil, nil, nil, uint64(gasUsed), errorWithMessage(err, errmsg)
	}
	return copyAndDestroyUnmanagedVector(res), copyAndDestroyUnmanagedVector(events), copyAndDestroyUnmanagedVector(attributes), uint64(gasUsed), nil
}

func IBCPacketTimeout(
	cache Cache,
	checksum []byte,
	env []byte,
	packet []byte,
	gasMeter *GasMeter,
	store KVStore,
	api *GoAPI,
	querier *Querier,
	gasLimit uint64,
	printDebug bool,
) ([]byte, []byte, []byte, uint64, error) {
	cs := makeView(checksum)
	defer runtime.KeepAlive(checksum)
	e := makeView(env)
	defer runtime.KeepAlive(env)
	pa := makeView(packet)
	defer runtime.KeepAlive(packet)

	callID := startCall()
	defer endCall(callID)

	dbState := buildDBState(store, callID)
	db := buildDB(&dbState, gasMeter)
	a := buildAPI(api)
	q := buildQuerier(querier)
	var gasUsed cu64
	errmsg := newUnmanagedVector(nil)
	events := newUnmanagedVector(nil)
	attributes := newUnmanagedVector(nil)

	res, err := C.ibc_packet_timeout(cache.ptr, cs, e, pa, db, a, q, cu64(gasLimit), cbool(printDebug), &gasUsed, &events, &attributes, &errmsg)
	if err != nil && err.(syscall.Errno) != C.ErrnoValue_Success {
		// Depending on the nature of the error, `gasUsed` will either have a meaningful value, or just 0.
		return nil, nil, nil, uint64(gasUsed), errorWithMessage(err, errmsg)
	}
	return copyAndDestroyUnmanagedVector(res), copyAndDestroyUnmanagedVector(events), copyAndDestroyUnmanagedVector(attributes), uint64(gasUsed), nil
}

// name: Serialized string
// args: Serialized [][]byte
// callstack: Serialized []string
// returned gasUsed: used gas without instantiation cost
func CallCallablePoint(
	name []byte,
	cache Cache,
	checksum []byte,
	isReadonly bool,
	callstack []byte,
	env []byte,
	args []byte,
	gasMeter *GasMeter,
	store KVStore,
	api *GoAPI,
	querier *Querier,
	gasLimit uint64,
	printDebug bool,
) ([]byte, []byte, []byte, uint64, error) {
	n := makeView(name)
	defer runtime.KeepAlive(name)
	cs := makeView(checksum)
	defer runtime.KeepAlive(checksum)
	e := makeView(env)
	defer runtime.KeepAlive(env)
	s := makeView(callstack)
	defer runtime.KeepAlive(callstack)
	as := makeView(args)
	defer runtime.KeepAlive(args)

	callID := startCall()
	defer endCall(callID)

	dbState := buildDBState(store, callID)
	db := buildDB(&dbState, gasMeter)
	a := buildAPI(api)
	q := buildQuerier(querier)
	var gasUsed cu64
	errmsg := newUnmanagedVector(nil)
	events := newUnmanagedVector(nil)
	attributes := newUnmanagedVector(nil)

	res, err := C.call_callable_point(n, cache.ptr, cs, cbool(isReadonly), s, e, as, db, a, q, cu64(gasLimit), cbool(printDebug), &gasUsed, &events, &attributes, &errmsg)
	if err != nil && err.(syscall.Errno) != C.ErrnoValue_Success {
		// Depending on the nature of the error, `gasUsed` will either have a meaningful value, or just 0.
		return nil, nil, nil, uint64(gasUsed), errorWithMessage(err, errmsg)
	}
	if isReadonly {
		return copyAndDestroyUnmanagedVector(res), nil, nil, uint64(gasUsed), nil
	} else {
		return copyAndDestroyUnmanagedVector(res), copyAndDestroyUnmanagedVector(events), copyAndDestroyUnmanagedVector(attributes), uint64(gasUsed), nil
	}
}

// returns: result, systemerr
//
//	result: serialized Option<String> which None means true, Some(e) means false and the reason is e.
func ValidateDynamicLinkInterface(
	cache Cache,
	checksum []byte,
	expectedInterface []byte,
) ([]byte, error) {
	cs := makeView(checksum)
	defer runtime.KeepAlive(checksum)
	ei := makeView(expectedInterface)
	defer runtime.KeepAlive(expectedInterface)
	errmsg := newUnmanagedVector(nil)
	res, err := C.validate_interface(cache.ptr, cs, ei, &errmsg)
	if err != nil && err.(syscall.Errno) != C.ErrnoValue_Success {
		return nil, errorWithMessage(err, errmsg)
	}
	return copyAndDestroyUnmanagedVector(res), nil
}

/**** To error module ***/

func errorWithMessage(err error, b C.UnmanagedVector) error {
	// this checks for out of gas as a special case
	if errno, ok := err.(syscall.Errno); ok && int(errno) == 2 {
		return types.OutOfGasError{}
	}
	msg := copyAndDestroyUnmanagedVector(b)
	if msg == nil {
		return err
	}
	return fmt.Errorf("%s", string(msg))
}
