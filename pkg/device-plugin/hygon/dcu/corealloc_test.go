package dcu

import (
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func TestInit(t *testing.T) {
	str := initCoreUsage(60)
	t.Log("str=", str)
	assert.Equal(t, strings.Compare(str, "000000000000000"), 0)
}

func TestAddCoreUsage(t *testing.T) {
	str := initCoreUsage(60)
	str1 := "abcde000ad00012"
	res, _ := addCoreUsage(str, str1)
	t.Log("res1=", res)
	assert.Equal(t, strings.Compare(res, str1), 0)
	str1 = "50200fff4000000"
	res, _ = addCoreUsage(res, str1)
	t.Log("res1=", res)
	assert.Equal(t, strings.Compare(res, "fbedefffed00012"), 0)
}

func TestAllocCoreUsage(t *testing.T) {
	str1 := "50200fff4000000"
	res, _ := allocCoreUsage(str1, 16)
	t.Log("res=", res)
	assert.Equal(t, strings.Compare(res, "afdfe0000000000"), 0)
	str1 = "abcde000ad00012"
	res, _ = allocCoreUsage(str1, 32)
	t.Log("res=", res)
}
