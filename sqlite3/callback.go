// Copyright 2018 The go-sqlite-lite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sqlite3

/*
#include "sqlite3.h"

// cgo doesn't handle SQLITE_{STATIC,TRANSIENT} pointer constants.
void _sqlite3_result_text(sqlite3_context* ctx, const char* s) {
  sqlite3_result_text(ctx, s, -1, SQLITE_STATIC);
}
void _sqlite3_result_blob(sqlite3_context* ctx, const void* b, int l) {
  sqlite3_result_blob(ctx, b, l, SQLITE_TRANSIENT);
}
*/
import "C"

import (
	"reflect"
	"unsafe"
)

type callbackArgCast struct {
	f   callbackArgConverter
	typ reflect.Type
}

func (c callbackArgCast) Run(v *C.sqlite3_value) (reflect.Value, error) {
	val, err := c.f(v)
	if err != nil {
		return reflect.Value{}, err
	}
	if !val.Type().ConvertibleTo(c.typ) {
		return reflect.Value{}, pkgErr(MISUSE, "cannot convert %s to %s", val.Type(), c.typ)
	}
	return val.Convert(c.typ), nil
}

func callbackArgInt64(v *C.sqlite3_value) (reflect.Value, error) {
	if C.sqlite3_value_type(v) != INTEGER {
		return reflect.Value{}, pkgErr(MISUSE, "argument must be an INTEGER")
	}
	return reflect.ValueOf(int64(C.sqlite3_value_int64(v))), nil
}

func callbackArgBool(v *C.sqlite3_value) (reflect.Value, error) {
	if C.sqlite3_value_type(v) != INTEGER {
		return reflect.Value{}, pkgErr(MISUSE, "argument must be an INTEGER")
	}
	i := int64(C.sqlite3_value_int64(v))
	val := false
	if i != 0 {
		val = true
	}
	return reflect.ValueOf(val), nil
}

func callbackArgFloat64(v *C.sqlite3_value) (reflect.Value, error) {
	if C.sqlite3_value_type(v) != FLOAT {
		return reflect.Value{}, pkgErr(MISUSE, "argument must be a FLOAT")
	}
	return reflect.ValueOf(float64(C.sqlite3_value_double(v))), nil
}

func callbackArgBytes(v *C.sqlite3_value) (reflect.Value, error) {
	switch C.sqlite3_value_type(v) {
	case BLOB:
		l := C.sqlite3_value_bytes(v)
		p := C.sqlite3_value_blob(v)
		return reflect.ValueOf(C.GoBytes(p, l)), nil
	case TEXT:
		l := C.sqlite3_value_bytes(v)
		c := unsafe.Pointer(C.sqlite3_value_text(v))
		return reflect.ValueOf(C.GoBytes(c, l)), nil
	default:
		return reflect.Value{}, pkgErr(MISUSE, "argument must be BLOB or TEXT")
	}
}

func callbackArgString(v *C.sqlite3_value) (reflect.Value, error) {
	switch C.sqlite3_value_type(v) {
	case BLOB:
		l := C.sqlite3_value_bytes(v)
		p := (*C.char)(C.sqlite3_value_blob(v))
		return reflect.ValueOf(C.GoStringN(p, l)), nil
	case TEXT:
		c := (*C.char)(unsafe.Pointer(C.sqlite3_value_text(v)))
		return reflect.ValueOf(C.GoString(c)), nil
	default:
		return reflect.Value{}, pkgErr(MISUSE, "argument must be BLOB or TEXT")
	}
}

func callbackArgGeneric(v *C.sqlite3_value) (reflect.Value, error) {
	switch C.sqlite3_value_type(v) {
	case INTEGER:
		return callbackArgInt64(v)
	case FLOAT:
		return callbackArgFloat64(v)
	case TEXT:
		return callbackArgString(v)
	case BLOB:
		return callbackArgBytes(v)
	case NULL:
		// Interpret NULL as a nil byte slice.
		var ret []byte
		return reflect.ValueOf(ret), nil
	default:
		panic("unreachable")
	}
}

func callbackArg(typ reflect.Type) (callbackArgConverter, error) {
	switch typ.Kind() {
	case reflect.Interface:
		if typ.NumMethod() != 0 {
			return nil, pkgErr(MISUSE, "the only supported interface type is interface{}")
		}
		return callbackArgGeneric, nil
	case reflect.Slice:
		if typ.Elem().Kind() != reflect.Uint8 {
			return nil, pkgErr(MISUSE, "the only supported slice type is []byte")
		}
		return callbackArgBytes, nil
	case reflect.String:
		return callbackArgString, nil
	case reflect.Bool:
		return callbackArgBool, nil
	case reflect.Int64:
		return callbackArgInt64, nil
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Int, reflect.Uint:
		c := callbackArgCast{callbackArgInt64, typ}
		return c.Run, nil
	case reflect.Float64:
		return callbackArgFloat64, nil
	case reflect.Float32:
		c := callbackArgCast{callbackArgFloat64, typ}
		return c.Run, nil
	default:
		return nil, pkgErr(MISUSE, "don't know how to convert to %s", typ)
	}
}

func callbackRetInteger(ctx *C.sqlite3_context, v reflect.Value) error {
	switch v.Type().Kind() {
	case reflect.Int64:
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Int, reflect.Uint:
		v = v.Convert(reflect.TypeOf(int64(0)))
	case reflect.Bool:
		b := v.Interface().(bool)
		if b {
			v = reflect.ValueOf(int64(1))
		} else {
			v = reflect.ValueOf(int64(0))
		}
	default:
		return pkgErr(MISUSE, "cannot convert %s to INTEGER", v.Type())
	}

	C.sqlite3_result_int64(ctx, C.sqlite3_int64(v.Interface().(int64)))
	return nil
}

func callbackRetFloat(ctx *C.sqlite3_context, v reflect.Value) error {
	switch v.Type().Kind() {
	case reflect.Float64:
	case reflect.Float32:
		v = v.Convert(reflect.TypeOf(float64(0)))
	default:
		return pkgErr(MISUSE, "cannot convert %s to FLOAT", v.Type())
	}

	C.sqlite3_result_double(ctx, C.double(v.Interface().(float64)))
	return nil
}

func callbackRetBlob(ctx *C.sqlite3_context, v reflect.Value) error {
	if v.Type().Kind() != reflect.Slice || v.Type().Elem().Kind() != reflect.Uint8 {
		return pkgErr(MISUSE, "cannot convert %s to BLOB", v.Type())
	}
	i := v.Interface()
	if i == nil || len(i.([]byte)) == 0 {
		C.sqlite3_result_null(ctx)
	} else {
		bs := i.([]byte)
		C._sqlite3_result_blob(ctx, unsafe.Pointer(&bs[0]), C.int(len(bs)))
	}
	return nil
}

func callbackRetText(ctx *C.sqlite3_context, v reflect.Value) error {
	if v.Type().Kind() != reflect.String {
		return pkgErr(MISUSE, "cannot convert %s to TEXT", v.Type())
	}
	C._sqlite3_result_text(ctx, cStr(v.Interface().(string)))
	return nil
}

func callbackRet(typ reflect.Type) (callbackRetConverter, error) {
	switch typ.Kind() {
	case reflect.Slice:
		if typ.Elem().Kind() != reflect.Uint8 {
			return nil, pkgErr(MISUSE, "the only supported slice type is []byte")
		}
		return callbackRetBlob, nil
	case reflect.String:
		return callbackRetText, nil
	case reflect.Bool, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Int, reflect.Uint:
		return callbackRetInteger, nil
	case reflect.Float32, reflect.Float64:
		return callbackRetFloat, nil
	default:
		return nil, pkgErr(MISUSE, "don't know how to convert to %s", typ)
	}
}

func callbackError(ctx *C.sqlite3_context, err error) {
	C.sqlite3_result_error(ctx, cStr(err.Error()), -1)
}
