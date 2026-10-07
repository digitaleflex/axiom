package docker

import (
	"reflect"
	"testing"
)

func TestDiscoverPorts(t *testing.T) {
	t.Run("single port", func(t *testing.T) {
		info := DiscoverPorts([]byte("FROM node:20-alpine\nEXPOSE 3000\n"))
		if !reflect.DeepEqual(info.Ports, []int{3000}) {
			t.Fatalf("ports = %v", info.Ports)
		}
		if info.Preferred != 3000 {
			t.Fatalf("preferred = %d", info.Preferred)
		}
		if info.HasCMD {
			t.Fatal("HasCMD must be false without CMD/ENTRYPOINT")
		}
	})

	t.Run("multiple ports, preferred is first", func(t *testing.T) {
		info := DiscoverPorts([]byte("FROM node:20-alpine\nEXPOSE 80 443 8080/tcp\n"))
		if !reflect.DeepEqual(info.Ports, []int{80, 443, 8080}) {
			t.Fatalf("ports = %v", info.Ports)
		}
		if info.Preferred != 80 {
			t.Fatalf("preferred = %d, want first port", info.Preferred)
		}
	})

	t.Run("multi-stage exposes merge in order", func(t *testing.T) {
		info := DiscoverPorts([]byte("FROM node:20-alpine AS build\nRUN npm run build\nFROM nginx:1.27-alpine\nEXPOSE 80\n"))
		if !reflect.DeepEqual(info.Ports, []int{80}) {
			t.Fatalf("ports = %v", info.Ports)
		}
		if info.Preferred != 80 {
			t.Fatalf("preferred = %d", info.Preferred)
		}
	})

	t.Run("no expose means no preferred port", func(t *testing.T) {
		info := DiscoverPorts([]byte("FROM node:20-alpine\nCMD [\"node\"]\n"))
		if len(info.Ports) != 0 || info.Preferred != 0 {
			t.Fatalf("ports = %v preferred = %d", info.Ports, info.Preferred)
		}
		if !info.HasCMD {
			t.Fatal("CMD must set HasCMD")
		}
	})

	t.Run("entrypoint sets HasCMD", func(t *testing.T) {
		info := DiscoverPorts([]byte("FROM node:20-alpine\nEXPOSE 3000\nENTRYPOINT [\"node\"]\n"))
		if !info.HasCMD {
			t.Fatal("ENTRYPOINT must set HasCMD")
		}
	})

	t.Run("variables are skipped", func(t *testing.T) {
		info := DiscoverPorts([]byte("FROM node:20-alpine\nEXPOSE $PORT\n"))
		if len(info.Ports) != 0 {
			t.Fatalf("variable ports must be skipped, got %v", info.Ports)
		}
	})
}
