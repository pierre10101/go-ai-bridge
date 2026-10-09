// A module of its own (refused fixture child_annotation): its schema.sql
// has malformed A5 inherited owner annotations, which the shared fixture
// app cannot have.
module example.com/childbad

go 1.24.0

require github.com/pierre10101/go-ai-bridge v0.7.0

replace github.com/pierre10101/go-ai-bridge => ../../../..
