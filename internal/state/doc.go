// Package state contains legacy domain projections and their isolated tests.
//
// The V1 runtime authority is cognitivecore.UniversalStore. This package must
// not be imported by Core, Discovery, API, or other runtime services until a
// reviewed migration explicitly gives it an authoritative role.
package state
