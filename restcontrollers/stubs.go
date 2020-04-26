package restcontrollers

import (
	"encoding/json"
	"fmt"
	"github.com/carvalhorr/protoc-gen-mock/stub"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"net/http"
)

const (
	contentType                = "Content-Type"
	contentTypeApplicationJson = "application/json"
	requestParamMethod         = "method"
	emptyString                = ""
)

type StubsController struct {
	StubsStore       stub.StubsStore
	SupportedMethods []string
	StubsValidators  []stub.StubsValidator
	StubExamples     []stub.Stub
}

func (c StubsController) GetHandlers() []RESTHandler {
	return []RESTHandler{
		{
			Name:    "GetStubs",
			Path:    "",
			Methods: []string{http.MethodGet},
			Handler: c.getStubsHandler,
		},
		{
			Name:    "AddStub",
			Path:    "",
			Methods: []string{http.MethodPost},
			Handler: c.addStubsHandler,
		},
		{
			Name:    "UpdateStub",
			Path:    "",
			Methods: []string{http.MethodPut},
			Handler: c.updateStubsHandler,
		},
		{
			Name:    "DeleteStub",
			Path:    "",
			Methods: []string{http.MethodDelete},
			Handler: c.deleteStubsHandler,
		},
	}
}

func (c StubsController) GetPath() string {
	return "/stubs"
}

func (c StubsController) getStubsHandler(writer http.ResponseWriter, request *http.Request) {
	log.Info("REST: received call to get stubs")

	method := getQueryParam(request, requestParamMethod)
	if method != emptyString && !c.isMethodSupported(method) {
		writeErrorResponse(writer, http.StatusBadRequest, fmt.Sprintf("Unsupported method: %s", method))
		return
	}

	stubs := c.getStubsFromStore(method)
	writeErr := writeResponse(writer, stubs)
	if writeErr != nil {
		writeErrorResponse(writer, http.StatusInternalServerError, writeErr.Error())
	}
}

func (c StubsController) addStubsHandler(writer http.ResponseWriter, request *http.Request) {
	s, err := readStubFromRequestBody(request)
	if err != nil {
		writeErrorResponse(writer, http.StatusBadRequest, fmt.Sprintf("call to add stubs failed with error: %s", err.Error()))
		return
	}
	log.WithFields(log.Fields{"stub": toJSON(s)}).
		Info("REST: received call to add stub")

	if !c.isMethodSupported(s.FullMethod) {
		writeErrorResponse(writer, http.StatusBadRequest, fmt.Sprintf("Method %s is not supported", s.FullMethod))
		return
	}
	isValid, errorMessages := c.isStubValid(s)
	if !isValid {
		invalidStubMessage := stub.InvalidStubMessage{
			Errors:  errorMessages,
			Example: *c.findExampleForMethod(s.FullMethod),
		}
		writeResponseWithCode(writer, invalidStubMessage, http.StatusBadRequest)
		return
	}

	if c.StubsStore.Exists(s) {
		writeErrorResponse(writer, http.StatusConflict, "Stub already exists")
		return
	}

	addErr := c.StubsStore.Add(s)
	if addErr != nil {
		log.Errorf("Failed to add stub %s -> %s. Error %s", s.FullMethod, s.Request.String(), addErr.Error())
		writeErrorResponse(writer, http.StatusInternalServerError, "Failed to update stub.")
		return
	}
	writeSuccessResponse(writer)
}

func (c StubsController) findExampleForMethod(method string) *stub.Stub {
	for _, stub := range c.StubExamples {
		if stub.FullMethod == method {
			return &stub
		}
	}
	return nil
}

func (c StubsController) updateStubsHandler(writer http.ResponseWriter, request *http.Request) {
	stub, err := readStubFromRequestBody(request)
	if err != nil {
		writeErrorResponse(writer, http.StatusBadRequest, fmt.Sprintf("call to update stub failed with error: %s", err.Error()))
		return
	}
	log.WithFields(log.Fields{"stub": toJSON(stub)}).
		Info("REST: received call to update stub")

	if !c.isMethodSupported(stub.FullMethod) {
		writeErrorResponse(writer, http.StatusBadRequest, fmt.Sprintf("Method %s is not supported", stub.FullMethod))
		return
	}

	if !c.StubsStore.Exists(stub) {
		writeErrorResponse(writer, http.StatusNotFound, "Stub not found")
		return
	}
	updateErr := c.StubsStore.Update(stub)
	if updateErr != nil {
		log.Errorf("Failed to update stub %s -> %s. Error %s", stub.FullMethod, stub.Request.String(), updateErr.Error())
		writeErrorResponse(writer, http.StatusInternalServerError, "Failed to update stub.")
		return
	}
	writeSuccessResponse(writer)
}

func (c StubsController) deleteStubsHandler(writer http.ResponseWriter, request *http.Request) {
	method := getQueryParam(request, requestParamMethod)
	if method != emptyString && !c.isMethodSupported(method) {
		writeErrorResponse(writer, http.StatusBadRequest, fmt.Sprintf("Can't delete stubs. Unsupported method: %s", method))
	}

	stub, err := readStubFromRequestBody(request)
	if err != nil {
		writeErrorResponse(writer, http.StatusBadRequest, fmt.Sprintf("call to delete stub failed with error: %s", err.Error()))
		return
	}
	log.WithFields(log.Fields{"stub": toJSON(stub), "method": method}).
		Info("REST: received call to delete stubs")

	switch {
	case method != emptyString:
		c.StubsStore.DeleteAllForMethod(method)
	case stub != nil:
		if !c.isMethodSupported(stub.FullMethod) {
			writeErrorResponse(writer, http.StatusBadRequest, fmt.Sprintf("Method %s is not supported", stub.FullMethod))
			return
		}

		if !c.StubsStore.Exists(stub) {
			writeErrorResponse(writer, http.StatusNotFound, "Stub not found")
			return
		}
		deleteErr := c.StubsStore.Delete(stub)
		if deleteErr != nil {
			log.Errorf("Failed to delete stub %s -> %s. Error %s", stub.FullMethod, stub.Request.String(), deleteErr.Error())
			writeErrorResponse(writer, http.StatusInternalServerError, "Failed to delete stub.")
		}
	default:
		c.StubsStore.DeleteAll()
	}

	writeSuccessResponse(writer)
}

func (c StubsController) isMethodSupported(method string) bool {
	for _, supportedMethod := range c.SupportedMethods {
		if supportedMethod == method {
			return true
		}
	}
	return false
}

func (c StubsController) getStubsFromStore(method string) []*stub.Stub {
	if method == emptyString {
		return c.StubsStore.GetAllStubs()
	}

	return c.StubsStore.GetStubsForMethod(method)
}

func (c StubsController) isStubValid(stub *stub.Stub) (isValid bool, errorMessages []string) {
	for _, validator := range c.StubsValidators {
		if isValid, errorMessages := validator.IsValid(stub); !isValid {
			return isValid, errorMessages
		}
	}
	return true, nil
}

func readStubFromRequestBody(request *http.Request) (*stub.Stub, error) {
	bodyData, err := ioutil.ReadAll(request.Body)
	if err != nil {
		log.Errorf("Unexpected error while reading stub from the request. Error %s", err.Error())
		return nil, fmt.Errorf("could not read stubs in payload")
	}
	defer request.Body.Close()

	if len(bodyData) == 0 {
		return nil, nil
	}

	stub := new(stub.Stub)
	unmarshalErr := json.Unmarshal(bodyData, stub)
	if unmarshalErr != nil {
		log.Errorf("Unexpected error while reading stub from the request. Error %s", unmarshalErr.Error())
		return nil, fmt.Errorf("could not read stubs in payload")
	}

	return stub, nil
}

func toJSON(p interface{}) string {
	str, _ := json.Marshal(p)
	return string(str)
}