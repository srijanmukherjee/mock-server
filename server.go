package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
)

const (
	MAX_DIRECTORY_COUNT = 20
	SERVER_NAME         = "mock-server"
)

var (
	mockRootDirectory = flag.String("mocks", "mocks", "reads mock files from this directory")
	port              = flag.Int("port", 9000, "mock server port")
	routeChain        = map[string]RouteChain{}
)

type MockDefinition struct {
	Endpoint string                 `json:"endpoint"`
	Method   string                 `json:"method"`
	Response MockResponseDefinition `json:"response"`
}

type MockResponseDefinition struct {
	Body       interface{}       `json:"body"`
	Headers    map[string]string `json:"headers"`
	StatusCode int               `json:"statusCode"`
}

type RouteChain struct {
	Handler    func(w http.ResponseWriter, r *http.Request)
	Middleware []ChainMiddleware
}

type ChainMiddleware struct {
	Method  string
	Handler func(w http.ResponseWriter, r *http.Request, next func(error))
}

func main() {
	flag.Parse()

	log.Printf("loading mocks from '%s'", *mockRootDirectory)

	mocks, err := loadMocks(*mockRootDirectory)
	if err != nil {
		log.Fatalf("failed to load mocks: %v", err)
	}

	log.Println("generating routes")

	mux := http.NewServeMux()

	for _, mock := range mocks {
		handler := generateMockHandler(mock)
		if handler != nil {
			mux.HandleFunc(mock.Endpoint, handler)
		}
	}

	log.Printf("server running at 0.0.0.0:%d", *port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%v", *port), mux))
}

func generateMockHandler(mock MockDefinition) func(w http.ResponseWriter, request *http.Request) {
	log.Printf("generating handler for %s %s", mock.Method, mock.Endpoint)

	chain, exist := routeChain[mock.Endpoint]
	if exist {
		chain.Middleware = append(chain.Middleware, ChainMiddleware{
			Method:  mock.Method,
			Handler: handleMockResponse(mock),
		})

		routeChain[mock.Endpoint] = RouteChain{
			Handler:    chain.Handler,
			Middleware: chain.Middleware,
		}

		return nil
	}

	handler := func(w http.ResponseWriter, r *http.Request) {
		middlewares := routeChain[mock.Endpoint].Middleware
		currentMidleware := -1

		var next func(error)

		next = func(err error) {
			if err != nil {
				log.Printf("[ERROR] %v", err)
				w.WriteHeader(500)
				return
			}

			currentMidleware = currentMidleware + 1

			if currentMidleware >= len(middlewares) {
				w.WriteHeader(404)
			} else if r.Method == middlewares[currentMidleware].Method {
				middleware := middlewares[currentMidleware]
				middleware.Handler(w, r, next)
			} else {
				next(nil)
			}
		}

		next(nil)
		log.Printf("%s %s", r.Method, r.Pattern)
	}

	chainMiddleware := make([]ChainMiddleware, 0)
	chainMiddleware = append(chainMiddleware, ChainMiddleware{
		Method:  mock.Method,
		Handler: handleMockResponse(mock),
	})

	routeChain[mock.Endpoint] = RouteChain{
		Handler:    handler,
		Middleware: chainMiddleware,
	}

	return handler
}

func handleMockResponse(mock MockDefinition) func(w http.ResponseWriter, r *http.Request, next func(error)) {
	statusCode := mock.Response.StatusCode
	if statusCode == 0 {
		statusCode = 200
	}

	serializedBody := serialize(mock.Response.Body)

	return func(w http.ResponseWriter, r *http.Request, next func(error)) {
		headers := map[string]string{
			"Content-Type": "application/json",
			"Server":       SERVER_NAME,
		}

		for key, value := range mock.Response.Headers {
			headers[key] = value
		}

		for key, value := range headers {
			w.Header().Add(key, value)
		}

		w.WriteHeader(statusCode)
		w.Write(serializedBody)
	}
}

func serialize(data interface{}) []byte {
	switch value := data.(type) {
	case map[string]interface{}:
		jsonBytes, err := json.Marshal(value)
		if err != nil {
			log.Fatalf("error while marshaling json: %v", err)
		}
		return jsonBytes
	default:
		return []byte(fmt.Sprint(value))
	}
}

func loadMocks(dirpath string) ([]MockDefinition, error) {
	mocks := make([]MockDefinition, 0)
	dirStack := make(chan string, MAX_DIRECTORY_COUNT)
	dirStack <- dirpath

	for len(dirStack) > 0 {
		if len(dirStack) > MAX_DIRECTORY_COUNT {
			return nil, fmt.Errorf("max directory count of %v exceeded", MAX_DIRECTORY_COUNT)
		}

		dirpath = <-dirStack

		entries, err := os.ReadDir(dirpath)
		if err != nil {
			return nil, err
		}

		for _, entry := range entries {
			if entry.IsDir() {
				dirStack <- path.Join(dirpath, entry.Name())
			} else if filepath.Ext(entry.Name()) == ".json" {
				mock, err := loadMockFromJson(path.Join(dirpath, entry.Name()))
				if err != nil {
					return nil, err
				}
				mocks = append(mocks, mock...)
			}
		}
	}

	return mocks, nil
}

func loadMockFromJson(filepath string) ([]MockDefinition, error) {
	log.Printf("loading mock %v", filepath)

	content, err := os.ReadFile(filepath)
	if err != nil {
		return nil, err
	}

	if !json.Valid(content) {
		return nil, fmt.Errorf("%v contains invalid json", filepath)
	}

	// TODO: check if it's an array or oject to provide better error message
	var listOfMocks []MockDefinition
	err = json.Unmarshal(content, &listOfMocks)
	if err == nil {
		return listOfMocks, nil
	}

	var mock MockDefinition
	err = json.Unmarshal(content, &mock)
	if err != nil {
		return nil, err
	}

	return []MockDefinition{mock}, nil
}

// func validateMock(mock MockDefinition) error {}
