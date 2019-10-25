package engine

import (
	"fmt"
	"time"
)

func Logger() HandlerFunc {
	return func(c *Context) {

		// Start time
		t := time.Now()

		// Process request
		c.Next()

		// Calculate request resolution time
		fmt.Printf("[%d] %s in %v\n", c.Writer.Status(), c.Req.RequestURI, time.Since(t))
	}
}
