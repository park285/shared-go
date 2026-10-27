package ginjson

import (
	"bytes"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// jsonRender는 gin render.Render 계약을 구현한다. Respond만 만들므로 공개하지 않는다.
type jsonRender struct {
	Data any
}

var jsonContentType = []string{"application/json; charset=utf-8"}

func (r jsonRender) Render(w http.ResponseWriter) error {
	var body bytes.Buffer

	if err := jsonv2.MarshalWrite(&body, r.Data, jsontext.EscapeForHTML(true)); err != nil {
		return fmt.Errorf("ginjson: marshal write: %w", err)
	}

	r.WriteContentType(w)

	if _, err := w.Write(body.Bytes()); err != nil {
		return fmt.Errorf("ginjson: write JSON body: %w", err)
	}

	return nil
}

func (r jsonRender) WriteContentType(w http.ResponseWriter) {
	header := w.Header()
	if val := header["Content-Type"]; len(val) == 0 {
		header["Content-Type"] = jsonContentType
	}
}

// Respond는 data를 HTML-safe encoding/json/v2 JSON으로 인코딩해 status와 함께 응답한다.
func Respond(c *gin.Context, status int, data any) {
	c.Render(status, jsonRender{Data: data})
}
