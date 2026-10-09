// A module of its own (refused fixture owner_annotation): its schema.sql has
// malformed A4 owner annotations, which the shared fixture app cannot have.
module example.com/ownerbad

go 1.24.0

require github.com/pierre10101/go-ai-bridge v0.4.0

replace github.com/pierre10101/go-ai-bridge => ../../../..
