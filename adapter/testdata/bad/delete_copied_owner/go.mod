// A module of its own (refused fixture delete_copied_owner): its schema.sql
// copies the owner column onto the child table, which the shared fixture
// app does not.
module example.com/deletecopied

go 1.24.0

require github.com/pierre10101/go-ai-bridge v0.7.0

replace github.com/pierre10101/go-ai-bridge => ../../../..
