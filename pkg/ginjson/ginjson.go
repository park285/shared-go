package ginjson

import (
	"bytes"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

type JSON struct {
	Data any
}

var jsonContentType = []string{"application/json; charset=utf-8"}

func (r JSON) Render(w http.ResponseWriter) error {
	var body bytes.Buffer

	if err := jsonv2.MarshalWrite(&body, r.Data, jsontext.EscapeForHTML(true)); err != nil {
		return fmt.Errorf("marshal write: %w", err)
	}

	r.WriteContentType(w)

	if _, err := w.Write(body.Bytes()); err != nil {
		return fmt.Errorf("write JSON body: %w", err)
	}

	return nil
}

func (r JSON) WriteContentType(w http.ResponseWriter) {
	header := w.Header()
	if val := header["Content-Type"]; len(val) == 0 {
		header["Content-Type"] = jsonContentType
	}
}

func Respond(c *gin.Context, status int, data any) {
	c.Render(status, JSON{Data: data})
}
