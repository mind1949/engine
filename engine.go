package engine

import (
	"encoding/json"
	"fmt"
	"github.com/julienschmidt/httprouter"
	"html/template"
	"math"
	"net/http"
	"path"
)

const (
	AbortIndex = math.MaxInt8 / 2
)

type (
	HandlerFunc = func(*Context)

	H map[string]interface{}

	// used internally to collect a error occurred during a http request
	ErrorMsg struct {
		Message string      `json:"message"`
		Meta    interface{} `json:"meta"`
	}

	ResponseWriter interface {
		http.ResponseWriter
		Status() int
		Written() bool
	}

	responseWriter struct {
		http.ResponseWriter
		status int
	}

	// context is the most important part of engine. it allow us to pass variables between middleware,
	// manage the flow, validate the JSON of a request and render a JSON response for example.
	Context struct {
		Req      *http.Request
		Writer   ResponseWriter
		Keys     map[string]interface{}
		Errors   []ErrorMsg
		Params   httprouter.Params
		handlers []HandlerFunc
		engine   *Engine
		index    int8
	}

	// used internally to configure router, a RouterGroup  is associated with a prefix
	// and an array of handlers(middleware)
	RouterGroup struct {
		Handlers []HandlerFunc
		prefix   string
		parent   *RouterGroup
		engine   *Engine
	}

	// Represents the web framework, it wrappers the blazing fast httprouter multiplexer and a list of global middleware
	Engine struct {
		*RouterGroup
		handlers404   []HandlerFunc
		router        *httprouter.Router
		HTMLTemplates *template.Template
	}
)

func (rw *responseWriter) WriteHeader(s int) {
	rw.ResponseWriter.WriteHeader(s)
	rw.status = s
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	return rw.ResponseWriter.Write(b)
}

func (rw *responseWriter) Status() int {
	return rw.status
}

func (rw *responseWriter) Written() bool {
	return rw.status != 0
}

// Return a new Blank Engine without any middleware attached
// the most basic configuration
func New() *Engine {
	engine := &Engine{}
	engine.RouterGroup = &RouterGroup{nil, "/", nil, engine}
	engine.router = httprouter.New()
	engine.router.NotFound = http.HandlerFunc(engine.handle404)
	return engine
}

// Return a Engine instance with the Logger and Recover middleware
func Default() *Engine {
	engine := New()
	engine.Use(Recovery(), Logger())
	return engine
}

func (engine *Engine) LoadHTMLTemplate(pattern string) {
	engine.HTMLTemplates = template.Must(template.ParseGlob(pattern))
}

// Add handlers for NotFound, It return 404 code by default
func (engine *Engine) NotFound404(handlers ...HandlerFunc) {
	engine.handlers404 = handlers
}

func (engine *Engine) handle404(w http.ResponseWriter, req *http.Request) {

	handlers := engine.allHandlers(engine.handlers404)
	c := engine.createContext(w, req, nil, handlers)
	c.Next()
	if !c.Writer.Written() {
		http.NotFound(w, req)
	}
}

// ServeHttp makes the router implement the http.Handler interface
func (engine *Engine) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	engine.router.ServeHTTP(w, req)
}

func (engine *Engine) Run(addr string) {
	http.ListenAndServe(addr, engine)
}

/************************************/
/********** ROUTES GROUPING *********/
/************************************/

func (group *RouterGroup) createContext(w http.ResponseWriter, req *http.Request, params httprouter.Params, handlers []HandlerFunc) *Context {
	return &Context{
		Writer:   &responseWriter{w, 0},
		Req:      req,
		index:    -1,
		engine:   group.engine,
		Params:   params,
		handlers: handlers,
	}
}

// Adds middleware to the group
func (group *RouterGroup) Use(middleware ...HandlerFunc) {
	group.Handlers = append(group.Handlers, middleware...)
}

// Greates a new router group. You should create add all the routes that share that have common middlwares or same path prefix.
// For example, all the routes that use a common middlware for authorization could be grouped.
func (group *RouterGroup) Group(component string, handlers ...HandlerFunc) *RouterGroup {
	prefix := path.Join(group.prefix, component)
	return &RouterGroup{
		Handlers: handlers,
		parent:   group,
		prefix:   prefix,
		engine:   group.engine,
	}
}

// Handle registers a new request handler and middleware with the given path and method.
// The laster handler should be the real handler, the other ones should be middleware that can and should be shared among different routes.
//
// For GET, POST, PUT, PATCH and DELETE requests the respective shortcut functions can be used.
//
// This function is intended for bulk loading and to allow the usage of less
// frequently used, non-standardized or custom methods (e.g. for internal
// communication with a proxy).
func (group *RouterGroup) Handle(method, p string, handlers []HandlerFunc) {
	p = path.Join(group.prefix, p)
	handlers = group.allHandlers(handlers)
	group.engine.router.Handle(method, p, func(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
		group.createContext(w, r, params, handlers).Next()
	})
}

// POST is the shortcut for router.Handle("POST", path, handle)
func (group *RouterGroup) POST(method, p string, handlers ...HandlerFunc) {
	group.Handle(method, p, handlers)
}

// GET is a shortcut for router.Handle("GET", path, handle)
func (group *RouterGroup) GET(path string, handlers ...HandlerFunc) {
	group.Handle("GET", path, handlers)
}

// DELETE is a shortcut for router.Handle("DELETE", path, handle)
func (group *RouterGroup) DELETE(path string, handlers ...HandlerFunc) {
	group.Handle("DELETE", path, handlers)
}

// PATCH is a shortcut for router.Handle("PATCH", path, handle)
func (group *RouterGroup) PATCH(path string, handlers ...HandlerFunc) {
	group.Handle("PATCH", path, handlers)
}

// PUT is a shortcut for router.Handle("PUT", path, handle)
func (group *RouterGroup) PUT(path string, handlers ...HandlerFunc) {
	group.Handle("PUT", path, handlers)
}

// Parent 测试
func (group *RouterGroup) Parent() *RouterGroup {
	return group.parent
}

func (group *RouterGroup) allHandlers(handlers []HandlerFunc) []HandlerFunc {
	local := append(group.Handlers, handlers...)
	if group.parent != nil {
		return group.allHandlers(local)
	} else {
		return local
	}
}

/************************************/
/****** FLOW AND ERROR MANAGEMENT****/
/************************************/

// Next should be used only in the middleware.
// It executes the pending handlers in the chain inside the calling handler.
func (c *Context) Next() {
	c.index++
	s := int8(len(c.handlers))
	for ; c.index < s; c.index++ {
		c.handlers[c.index](c)
	}
}

// Forces the system to do not continue calling the pending handlers.
// For example, the first handler checks if the request is authorized. If it's not , context.Abort(401) shold be called.
// The rest of pending handlers would never be called for that request.
func (c *Context) Abort(code int) {
	c.Writer.WriteHeader(code)
	c.index = AbortIndex
}

// Fail is the same than Abort plus an error message.
// Calling `context.Fail(500, err)` is equivalent to:
// ```
// context.Error("Operation aborted", err)
// context.Abort(500)
// ```
func (c *Context) Fail(code int, err error) {
	c.Error(err, "Operation aborted")
	c.Abort(code)
}

// Attaches an error to the current context. the error is pushed to a list of errors.
// It's a good idea to call Error for each error occurred during the resolution of a request.
// A middleware can be used to collect all the errors and push them to a database together, print a log, or append it in the HTTP response.
func (c *Context) Error(err error, meta interface{}) {
	c.Errors = append(c.Errors, ErrorMsg{
		Message: err.Error(),
		Meta:    meta,
	})
}

/************************************/
/******** METADATA MANAGEMENT********/
/************************************/

// Sets a new pair key/value just for the specified context
// It also lazy initializes the hashmap
func (c *Context) Set(key string, value interface{}) {
	if c.Keys == nil {
		c.Keys = map[string]interface{}{}
	}
	c.Keys[key] = value
}

// Returns the value for the given key.
// It panics if the value doesn't dexist.
func (c *Context) Get(key string) interface{} {
	var item interface{}
	var ok bool
	if c.Keys != nil {
		item, ok = c.Keys[key]
	} else {
		item, ok = nil, false
	}
	if !ok || item == nil {
		panic(fmt.Sprintf("Keys %s doesn't exist", key))
	}

	return item
}

/************************************/
/******** ENCODING MANAGEMENT********/
/************************************/

// Like ParseBody() but this method also writes a 400 error if the json is not valid.
func (c *Context) EnsureBody(item interface{}) bool {
	if err := c.ParseBody(item); err != nil {
		c.Fail(400, err)
		return false
	}
	return true
}

// Parses the body content as a JSON input. It decodes the json payload into the struct specified as a pointer.
func (c *Context) ParseBody(item interface{}) error {
	decoder := json.NewDecoder(c.Req.Body)
	if err := decoder.Decode(&item); err != nil {
		return Validate(c, item)
	} else {
		return err
	}
}

// Serializes the given struct as a JSON into the response body in a fast and efficient way.
// It also sets the Content-Type as "application/json"
func (c *Context) JSON(code int, obj interface{}) {
	c.Writer.WriteHeader(code)
	c.Writer.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(c.Writer)
	if err := encoder.Encode(obj); err != nil {
		c.Error(err, obj)
		http.Error(c.Writer, err.Error(), 500)
	}
}

// Serializes the given struct as XML into the response body in a fast and efficient way
// It also sets the Content-Type as "application/json"
func (c *Context) XML(code int, obj interface{}) {
	c.Writer.WriteHeader(code)
	c.Writer.Header().Set("Content-Type", "application/xml")
	encoder := json.NewEncoder(c.Writer)
	if err := encoder.Encode(obj); err != nil {
		c.Error(err, obj)
		http.Error(c.Writer, err.Error(), 500)
	}
}

// Renders the html template specified by his file name.
// It also update the http code and set the Content-Type as "application/html"
func (c *Context) HTML(code int, name string, data interface{}) {
	c.Writer.WriteHeader(code)
	c.Writer.Header().Set("Content-Type", "application/html")
	if err := c.engine.HTMLTemplates.ExecuteTemplate(c.Writer, name, data); err != nil {
		c.Error(err, map[string]interface{}{
			"name": name,
			"data": data,
		})
		http.Error(c.Writer, err.Error(), 500)
	}
}

// Writes the given string into the response body and set the Content-Type to "application/plain"
func (c *Context) String(code int, msg string) {
	c.Writer.WriteHeader(code)
	c.Writer.Header().Set("Content-Type", "application/plain")
	c.Writer.Write([]byte(msg))
}

// Writes some data into the body stream and updates status code
func (c *Context) Data(code int, data []byte) {
	c.Writer.WriteHeader(code)
	c.Writer.Write(data)
}
