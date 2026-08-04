package main

import (
	"html/template"
	"testing"
)

func TestTemplatesParseWithChatPage(t *testing.T) {
	_, err := template.New("").Funcs(template.FuncMap{
		"safe": func(value string) template.HTML { return template.HTML(value) },
	}).ParseGlob("templates/*")
	if err != nil {
		t.Fatalf("templates must parse: %v", err)
	}
}
