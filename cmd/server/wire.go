package main

// wire.go: boot ordering stub for Phase 8 (C12).
// The deterministic boot order will be:
//   1. migrate (if --migrate flag)
//   2. sealer init
//   3. DB pools
//   4. bus
//   5. reconciler
//   6. providers (whatsmeow per tenant)
//   7. HTTP server
