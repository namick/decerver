package runtimemanager


import (
	"fmt"
	"github.com/eris-ltd/decerver/interfaces/events"
	"github.com/eris-ltd/decerver/interfaces/logging"
	"github.com/eris-ltd/decerver/interfaces/scripting"
	"github.com/eris-ltd/decerver/interfaces/types"
	"github.com/robertkrimen/otto"
	"io/ioutil"
	"log"
	"sync"
)

var logger *log.Logger = logging.NewLogger("ScriptEngine")

//type RuntimeEventProcessor struct {
//	er events.EventProcessor
//}

type JsObj struct {
	Name   string
	Object interface{}
}

// Implements RuntimeManager
type RuntimeManager struct {
	runtimes  map[string]scripting.Runtime
	apiObjs   []*JsObj
	apiScript []string
	ep        events.EventProcessor
}

func NewRuntimeManager(ep events.EventProcessor) scripting.RuntimeManager {
	return &RuntimeManager{
		make(map[string]scripting.Runtime),
		make([]*JsObj, 0),
		make([]string, 0),
		ep,
	}
}

func (rm *RuntimeManager) ShutdownRuntimes() {
	for _, rt := range rm.runtimes {
		rt.Shutdown()
	}
}

func (rm *RuntimeManager) CreateRuntime(name string) scripting.Runtime {
	rt := newRuntime(name, rm.ep)
	rm.runtimes[name] = rt

	rt.Init(name)
	for _, jo := range rm.apiObjs {
		err := rt.BindScriptObject(jo.Name, jo.Object)
		if err != nil {
			fmt.Println(err.Error())
		}
	}
	for _, s := range rm.apiScript {
		err := rt.AddScript(s)
		if err != nil {
			fmt.Println(err.Error())
		}
	}

	logger.Printf("Creating new runtime: " + name)
	// DEBUG
	logger.Printf("Runtimes: %v\n", rm.runtimes)
	return rt
}

func (rm *RuntimeManager) GetRuntime(name string) scripting.Runtime {
	rt, ok := rm.runtimes[name]
	if ok {
		return rt
	} else {
		return nil
	}
}

func (rm *RuntimeManager) RemoveRuntime(name string) {
	rt, ok := rm.runtimes[name]
	if ok {
		delete(rm.runtimes, name)
		rt.Shutdown()
	}
}

func (rm *RuntimeManager) RegisterApiObject(objectname string, api interface{}) {
	rm.apiObjs = append(rm.apiObjs, &JsObj{objectname, api})
}

func (rm *RuntimeManager) RegisterApiScript(script string) {
	rm.apiScript = append(rm.apiScript, script)
}

// Implements interface scripts.Runtime
type Runtime struct {
	vm            *otto.Otto
	ep            events.EventProcessor
	name          string
	mutex         *sync.Mutex
	lockLvl       int
}

// Package private
func newRuntime(name string, ep events.EventProcessor) scripting.Runtime {
	vm := otto.New()
	rt := &Runtime{}
	rt.vm = vm
	rt.ep = ep
	rt.name = name
	rt.mutex = &sync.Mutex{}
	return rt
}

func (rt *Runtime) Shutdown() {
	logger.Println("Runtime shut down: " + rt.name)
	// TODO implement
}

// TODO add an interrupt channel.
func (rt *Runtime) Init(name string) {
	// Bind an event subscribe function to otto
	rt.vm.Set("events_subscribe", func(call otto.FunctionCall) otto.Value {
	    // TODO Error checking
	    source, _ := call.Argument(0).ToString()
		tpe, _ := call.Argument(1).ToString()
		target, _ := call.Argument(2).ToString()
		id, _ := call.Argument(3).ToString()
		rtSub := newRuntimeSub(source,tpe,target,id, rt)
		rt.ep.Subscribe(rtSub)
	    return otto.Value{}
	})
	// Bind an event unsubscribe function to otto
	rt.vm.Set("events_unsubscribe", func(call otto.FunctionCall) otto.Value {
	    id, _ := call.Argument(0).ToString()
	    rt.ep.Unsubscribe(id)
	    return otto.Value{}
	})
	// Bind the runtime id (it's name)
	rt.vm.Set("RuntimeId", name)
	// Bind all the normal things.
	BindDefaults(rt)
}

// TODO link with fileIO
func (rt *Runtime) LoadScriptFile(fileName string) error {
	rt.mutex.Lock()
	defer rt.mutex.Unlock()
	bytes, err := ioutil.ReadFile(fileName)
	if err != nil {
		return err
	}
	_, err = rt.vm.Run(bytes)
	return err
}

func (rt *Runtime) LoadScriptFiles(fileName ...string) error {
	for _, sf := range fileName {
		err := rt.LoadScriptFile(sf)
		if err != nil {
			return err
		}
	}
	return nil
}

func (rt *Runtime) BindScriptObject(name string, val interface{}) error {
	rt.mutex.Lock()
	defer rt.mutex.Unlock()
	err := rt.vm.Set(name, val)
	return err
}

func (rt *Runtime) AddScript(script string) error {
	rt.mutex.Lock()
	defer rt.mutex.Unlock()
	_, err := rt.vm.Run(script)
	return err
}

func (rt *Runtime) CallFuncOnObj(objName, funcName string, param ...interface{}) (interface{}, error) {
	rt.mutex.Lock()
	defer rt.mutex.Unlock()
	ob, err := rt.vm.Get(objName)
	if err != nil {
		fmt.Println(err.Error())
		return nil, err
	}

	val, callErr := ob.Object().Call(funcName, param...)

	if callErr != nil {
		fmt.Println(callErr.Error())
		return nil, err
	}

	// Take the result and turn it into a go value.
	obj, expErr := val.Export()

	if expErr != nil {
		return nil, fmt.Errorf("Error when exporting returned value: %s\n", expErr.Error())
	}
	return obj, nil
}

func (rt *Runtime) CallFunc(funcName string, param ...interface{}) (interface{}, error) {
	rt.mutex.Lock()
	defer rt.mutex.Unlock()
	val, callErr := rt.vm.Call(funcName, nil, param)

	if callErr != nil {
		fmt.Println(callErr.Error())
		return nil, callErr
	}

	fmt.Printf("%v\n", val)

	// Take the result and turn it into a go value.
	obj, expErr := val.Export()

	if expErr != nil {
		return nil, fmt.Errorf("Error when exporting returned value: %s\n", expErr.Error())
	}

	return obj, nil
}

// Will be refactored asap. See events/events.go for an explanation.
type RuntimeSub struct {
	source    string
	tpe       string
	tgt       string
	id        string
	rt        scripting.Runtime
}

func newRuntimeSub(eventSource, eventType, eventTarget, subId string, rt scripting.Runtime) *RuntimeSub {
	rs := &RuntimeSub{}
	rs.source = eventSource
	rs.tpe = eventType
	rs.tgt = eventTarget
	rs.id = subId
	rs.rt = rt
	return rs
}

func (rs *RuntimeSub) Source() string {
	return rs.source
}

func (rs *RuntimeSub) Id() string {
	return rs.id
}

func (rs *RuntimeSub) Target() string {
	return rs.tgt
}

func (rs *RuntimeSub) Event() string {
	return rs.tpe
}

// Passing along the sub ID means the right callback is used.
func (rs *RuntimeSub) Post(e events.Event) {
	rs.rt.CallFuncOnObj("events", "post", rs.id, types.ToJsValue(e) )
}
