package engine

import (
	"log"
	"time"
)

func Logger() HandlerFunc {
	return func(c *Context) {

		// Start time
		t := time.Now()

		// Process request
		c.Next()

		// Calculate request resolution time
		log.Printf("%s in %v", c.Req.RequestURI, time.Since(t))
	}
}
