package ate

import (
	"encoding/hex"
	"fmt"
	"github.com/obscuren/sha3"
	"github.com/robertkrimen/otto"
	//"github.com/eris-ltd/decerver-interfaces/events"
	"github.com/eris-ltd/decerver-interfaces/core"
	"log"
	"math/big"
	"time"
)

var BZERO *big.Int = big.NewInt(0)

func isZero(i *big.Int) bool {
	return i.Cmp(BZERO) == 0
}

var ottoLog *log.Logger = core.NewLogger("JsRuntime")

func BindDefaults(runtime *JsRuntime) {
	vm := runtime.vm

	var err error

	bindHelpers(vm)

	// Networking.
	_, err = vm.Run(`
		
		var E_PARSE = -32700;
		var E_INVALID_REQ = -32600;
		var	E_NO_METHOD = -32601;
		var	E_BAD_PARAMS = -32602;
		var	E_INTERNAL = -32603;
		var	E_SERVER = -32000;
		
		// Network is an object that encapsulates all networking activity.
		var network = {};
		
		// Http...
		
		network.incomingHttpCallback = function(){};
		
		// Used internally.
		network.handleIncomingHttp = function(httpReqAsJson){
			var httpReq = JSON.parse(reqAsJson);
			this.incomingHttpCallback(httpReq);
		};
		
		network.registerIncomingHttpCallback = function(callback){
			if(typeof handler !== "function"){
				throw Error("Attempting to register a non-function as incoming http handler");
			}
			network.httpHandler = handler;
		}
		
		// Websockets
		
		// Each session has a handler.
		network.wsHandlers = {};
		network.wsSessions = {};
		
		network.newWsCallback = function(sessionObj){
			return function (){
				Println("No callback registered for new websocket connections.");
			};
		};
		
		network.newWsSession = function(sessionObj){
			var sId = sessionObj.SessionId();
			Println("Adding new session: " + sId);
			network.wsHandlers[sId] = network.newWsCallback(sessionObj);
			network.wsSessions[sId] = sessionObj;
		}
		
		network.deleteWsCallback = function(sessionObj){
			return function (){
				Println("No callback registered for delete websocket connections.");
			};
		};
		
		network.deleteWsSession = function(sessionId){
			var sId = sessionId;
			var sessionObj = network.wsSessions[sId];
			if(typeof network.wsSessions[sId] === "undefined" || network.wsSessions[sId] === null){
				Println("No session with id " + sId + ". Cannot delete.");
				return;
			}
			Println("Deleting session: " + sId);
			network.wsSessions[sId] = null;
			network.deleteWsCallback(sessionObj);
		}
		
		network.incomingWsMsg = function(sessionId, reqJson) {
			Println("Incoming websocket message.");
			try {
				var request = JSON.parse(reqJson);
				if (typeof(request.Method) === "undefined" || request.Method === ""){
					return JSON.stringify(network.getWsError(E_NO_METHOD, "No method in request."));
				} else {
					var handler = network.wsHandlers[sessionId];
					if (typeof handler !== "function"){
						return JSON.stringify(network.getWsError(E_SERVER, "Handler not registered for websocket session: " + sessionId.toString()) );
					}
					var response = handler(request);
					if(response === null){
						return null;
					}
					var respStr;
					try {
						response.Time = TimeMS();
						respStr = JSON.stringify(response);
					} catch (err) {
						return JSON.stringify(network.getWsError(E_INTERNAL, "Failed to marshal response: " + err));
					}
					return respStr;
				}
			} catch (err){
				response = JSON.stringify(network.getWsError(E_PARSE, err));
			}
		}
		
		network.newWsRequest = function(){
			return {
				"Protocol" : "EWSMP1",
				"Method" : "",
				"Params" : "",
				"Time" : "",
				"Id" : ""
			};
		}
		
		network.getWsResponse = function(){
			return {
				"Protocol" : "EWSMP1",
				"Method" : "",
				"Result" : "",
				"Error" : "",
				"Time" : "",
				"Id" : ""
			};
		}
		
		network.getWsErrorDetailed = function(code, message, data){
			return {
				"Protocol" : "EWSMP1",
				"Method" : "",
				"Result" : "",
				"Time" : "",
				"Id" : "",
				"Error" : {
					"Code" : code,
					"Message" : message,
					"Data" : data
				  }
			};
		}
		
		network.getWsError = function(error){
			if (typeof(error) !== "string") {
				error = "Server passed non string to error function (bad server-side javascript).";
			}
			return {
				"Protocol" : "EWSMP1",
				"Method" : "",
				"Result" : "",
				"Timestamp" : "",
				"Id" : "",
				"Error" : {
					"Code" : E_INTERNAL,
					"Message" : error,
					"Data" : null
				  }
			};
		}
	`)

	if err != nil {
		fmt.Println("[Atë] Error while bootstrapping js networking: " + err.Error())
	} else {
		fmt.Println("[Atë] Networking script loaded.")
	}

	_, err = vm.Run(`
	
		var events = {};
		
		events.callbacks = {};
		
		events.subscribe = function(eventSource, eventType, eventTarget, callbackFn){
		
			if(typeof(callbackFn) !== "function"){
				throw new Error("Trying to register a non callback function as callback.");
			}
			
			var eventId = events.generateId(eventSource,eventType);
			// The jsr_events object has the go bindings to actually subscribe.
			jsr_events.Subscribe(eventSource, eventType, eventTarget, eventId);
			this.callbacks[eventId] = callbackFn;	
		}
		
		events.unsubscribe = function(eventSource,eventName){
			var subId = events.generateId(eventSource,eventName);
			jsr_events.Unsubscribe(subId);
			events.callbacks[subId] = null;
		}
		
		// Called by the go event processor.
		events.post = function(eventJson){
			
			var event = JSON.parse(eventJson);			
			var eventId = events.generateId(event.Source, event.Event);
			var cfn = this.callbacks[eventId];
			if (typeof(cfn) === "function"){
				cfn(event);
			} else {
				Println("No callback for event: " + eventId);
			}
			return;
		}
		
		events.generateId = function(evtSource,evtName){
			return RuntimeId + "_" + evtSource + "_" + evtName; 
		}
	`)

	if err != nil {
		fmt.Println("[Atë] Error while bootstrapping js event-processing: " + err.Error())
	} else {
		fmt.Println("[Atë] Event processing script loaded.")
	}

}

func bindHelpers(vm *otto.Otto) {

	vm.Set("Add", func(call otto.FunctionCall) otto.Value {
		p0, p1, errP := parseBin(call)
		if errP != nil {
			return otto.UndefinedValue()
		}
		result, _ := vm.ToValue("0x" + hex.EncodeToString(p0.Add(p0, p1).Bytes()))
		return result
	})

	vm.Set("Sub", func(call otto.FunctionCall) otto.Value {
		p0, p1, errP := parseBin(call)
		if errP != nil {
			return otto.UndefinedValue()
		}
		p0.Sub(p0, p1)
		if p0.Sign() < 0 {
			otto.NaNValue() // TODO
		}
		result, _ := vm.ToValue("0x" + hex.EncodeToString(p0.Bytes()))
		return result
	})

	vm.Set("Mul", func(call otto.FunctionCall) otto.Value {
		p0, p1, errP := parseBin(call)
		if errP != nil {
			return otto.UndefinedValue()
		}
		result, _ := vm.ToValue("0x" + hex.EncodeToString(p0.Mul(p0, p1).Bytes()))
		return result
	})

	vm.Set("Div", func(call otto.FunctionCall) otto.Value {
		p0, p1, errP := parseBin(call)
		if errP != nil {
			return otto.UndefinedValue()
		}
		fmt.Println("DIV Nom: " + p0.String())
		fmt.Println("Div Denom: " + p1.String())
		if isZero(p1) {
			return otto.NaNValue()
		}
		result, _ := vm.ToValue("0x" + hex.EncodeToString(p0.Div(p0, p1).Bytes()))
		return result
	})

	vm.Set("Mod", func(call otto.FunctionCall) otto.Value {
		p0, p1, errP := parseBin(call)
		if errP != nil {
			return otto.UndefinedValue()
		}
		if isZero(p1) {
			return otto.NaNValue()
		}
		result, _ := vm.ToValue("0x" + hex.EncodeToString(p0.Mod(p0, p1).Bytes()))
		return result
	})
	
	vm.Set("Equals", func(call otto.FunctionCall) otto.Value {
		p0, p1, errP := parseBin(call)
		if errP != nil {
			return otto.UndefinedValue()
		}
		ret := false
		if p0.Cmp(p1) == 0 {
			ret = true;
		}
		result, _ := vm.ToValue(ret)
		return result
	})

	vm.Set("Exp", func(call otto.FunctionCall) otto.Value {
		p0, p1, errP := parseBin(call)
		if errP != nil {
			return otto.UndefinedValue()
		}
		result, _ := vm.ToValue("0x" + hex.EncodeToString(p0.Exp(p0, p1, nil).Bytes()))
		// fmt.Println("[OTTOTOTOOTT] " + )
		return result
	})

	vm.Set("IsZero", func(call otto.FunctionCall) otto.Value {
		prm, err0 := call.Argument(0).ToString()
		if err0 != nil {
			return otto.UndefinedValue()
		}
		isZero := prm == "0" || prm == "0x" || prm == "0x0"
		result, _ := vm.ToValue(isZero)

		return result
	})

	vm.Set("HexToString", func(call otto.FunctionCall) otto.Value {
		prm, err0 := call.Argument(0).ToString()
		if err0 != nil {
			return otto.UndefinedValue()
		}
		bts, err1 := hex.DecodeString(prm)
		if err1 != nil {
			return otto.UndefinedValue()
		}
		result, _ := vm.ToValue(string(bts))

		return result
	})

	vm.Set("StringToHex", func(call otto.FunctionCall) otto.Value {
		prm, err0 := call.Argument(0).ToString()
		fmt.Println("[OTTO] String: " + prm)
		if err0 != nil {
			return otto.UndefinedValue()
		}
		bts := []byte(prm)
		
		if 32 > len(bts) {
			zeros := make([]byte, 32 - len(bts) )
			bts = append(zeros,bts...)
		}
		res := "0x" + hex.EncodeToString(bts)
		fmt.Println("[OTTO] String hex: " + res)
		fmt.Printf("[OTTO] len: %d\n", len(bts))
		result, _ := vm.ToValue(res)
		
		return result
	})

	// Millisecond time.
	vm.Set("TimeMS", func(call otto.FunctionCall) otto.Value {
		ts := time.Now().UnixNano() >> 6
		result, _ := vm.ToValue(ts)
		return result
	})

	// Crypto
	vm.Set("SHA3", func(call otto.FunctionCall) otto.Value {
		prm, err0 := call.Argument(0).ToString()
		if err0 != nil {
			return otto.UndefinedValue()
		}
		if prm[1] == 'x' {
			prm = prm[2:]
		}
		h, err := hex.DecodeString(prm)
		fmt.Printf("Hashed value: %s\n", string(h))
		if err != nil {
			fmt.Printf("Error hashing, " + err.Error())
			return otto.UndefinedValue()
		}
		d := sha3.NewKeccak256()
		d.Write(h)
		v := hex.EncodeToString(d.Sum(nil))
		fmt.Println("SHA3: " + v)
		result, _ := vm.ToValue("0x" + v)

		return result
	})

	vm.Set("Print", func(call otto.FunctionCall) otto.Value {
		output := make([]interface{}, 0)
		// TODO error
		for _, argument := range call.ArgumentList {
			arg, _ := argument.Export()
			output = append(output, arg)
		}
		ottoLog.Print(output...)
		return otto.Value{}
	})

	vm.Set("Println", func(call otto.FunctionCall) otto.Value {
		output := make([]interface{}, 0)
		// TODO error
		for _, argument := range call.ArgumentList {
			arg, _ := argument.Export()
			output = append(output, arg)
		}
		ottoLog.Println(output...)
		return otto.Value{}
	})

	vm.Set("Printf", func(call otto.FunctionCall) otto.Value {
		args := call.ArgumentList
		if args == nil || len(args) == 0 {
			ottoLog.Println("")
			return otto.Value{}
		}
		fmtStr, _ := args[0].Export()
		fs, ok := fmtStr.(string)
		if !ok {
			ottoLog.Println("")
			return otto.Value{}
		}

		if len(args) == 1 {
			ottoLog.Printf(fs)
		} else {
			output := make([]interface{}, 0)
			// TODO error
			for _, argument := range call.ArgumentList[1:] {
				arg, _ := argument.Export()
				output = append(output, arg)
			}
			ottoLog.Printf(fs, output...)
		}
		return otto.Value{}
	})
}

func parseUn(call otto.FunctionCall) (*big.Int, error) {
	str, err0 := call.Argument(0).ToString()
	if err0 != nil {
		return nil, err0
	}
	val := atob(str)
	return val, nil
}

func parseBin(call otto.FunctionCall) (*big.Int, *big.Int, error) {
	left, err0 := call.Argument(0).ToString()
	if err0 != nil {
		return nil, nil, err0
	}
	right, err1 := call.Argument(1).ToString()

	if err1 != nil {
		return nil, nil, err1
	}
	p0 := atob(left)
	p1 := atob(right)
	return p0, p1, nil
}

func atob(str string) *big.Int {
	i := new(big.Int)
	fmt.Sscan(str, i)
	return i
}