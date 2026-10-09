// Package main declares the app's roles (A2) for this refused fixture.
package main

import "github.com/pierre10101/go-ai-bridge/runtime/httpx"

var AppRoles = httpx.AppRoles("organizer", "admin").BypassOwnership("admin")
