package adt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ZCL_ADT_HTTP_GATE on ZED (7.50), 2026-10-11: the objectstructure types its
// components CLAS/OR, CLAS/OM and CLAS/OA, and the global DEFINITION statement
// spans lines, with PUBLIC between DEFINITION and FINAL.
func TestGetClassInfoReadsA750ClassStructure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-csrf-token", "test-token")
		switch {
		case strings.HasSuffix(r.URL.Path, "/objectstructure"):
			w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?>
<abapsource:objectStructureElement xmlns:abapsource="http://www.sap.com/adt/abapsource" xmlns:adtcore="http://www.sap.com/adt/core" adtcore:name="ZCL_ADT_HTTP_GATE" adtcore:type="CLAS/OC" visibility="public">
  <abapsource:objectStructureElement adtcore:name="IF_HTTP_EXTENSION" adtcore:type="CLAS/OR" visibility="public"/>
  <abapsource:objectStructureElement adtcore:name="IS_AUTHORIZED" adtcore:type="CLAS/OM" visibility="private" level="instance"/>
  <abapsource:objectStructureElement adtcore:name="C_PROFILE" adtcore:type="CLAS/OA" visibility="private" level="static"/>
</abapsource:objectStructureElement>`))
		case strings.HasSuffix(r.URL.Path, "ZCL_CHILD/source/main"):
			w.Write([]byte("CLASS zcl_child DEFINITION\n  PUBLIC\n  INHERITING FROM zcl_base\n  ABSTRACT\n  CREATE PUBLIC .\n"))
		case strings.HasSuffix(r.URL.Path, "/source/main"):
			w.Write([]byte("CLASS zcl_adt_http_gate DEFINITION\r\n  PUBLIC\r\n  FINAL\r\n  CREATE PUBLIC.\r\n\r\n  PUBLIC SECTION.\r\nENDCLASS.\r\n"))
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "user", "pass")
	info, err := c.GetClassInfo(context.Background(), "ZCL_ADT_HTTP_GATE")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(info.Methods, ",") != "IS_AUTHORIZED" ||
		strings.Join(info.Attributes, ",") != "C_PROFILE" ||
		strings.Join(info.Interfaces, ",") != "IF_HTTP_EXTENSION" {
		t.Errorf("methods %v, attributes %v, interfaces %v", info.Methods, info.Attributes, info.Interfaces)
	}
	if !info.IsFinal || info.IsAbstract || info.Category != "Final" {
		t.Errorf("isFinal %v, isAbstract %v, category %q: want a FINAL class", info.IsFinal, info.IsAbstract, info.Category)
	}

	if info, err = c.GetClassInfo(context.Background(), "ZCL_CHILD"); err != nil {
		t.Fatal(err)
	}
	if !info.IsAbstract || info.IsFinal || info.Superclass != "ZCL_BASE" {
		t.Errorf("isAbstract %v, isFinal %v, superclass %q: want an abstract subclass of ZCL_BASE", info.IsAbstract, info.IsFinal, info.Superclass)
	}
}
