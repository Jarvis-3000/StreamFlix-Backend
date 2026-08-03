package media

// indexMapping is the explicit mapping for the media index. Dynamic mapping is
// disabled ("dynamic": "strict") so any field not declared here is rejected at
// index time — this keeps the schema honest and forces mapping changes to be
// deliberate.
//
// Field type choices:
//   - keyword  : exact-match / filter / aggregate (ids, urls, enums, tags)
//   - text     : full-text search (title, description)
//   - date     : timestamps
//   - integer  : duration in seconds
//
// title also carries a keyword sub-field (title.keyword) for exact-match and
// sorting alongside full-text search.
//
// Every field of models.Media must appear here, and adding one there without
// adding it here makes Elasticsearch reject the write outright.
const indexMapping = `{
  "mappings": {
    "dynamic": "strict",
    "properties": {
      "id":                  { "type": "keyword" },
      "creator_id":          { "type": "keyword" },
      "title":               { "type": "text", "fields": { "keyword": { "type": "keyword", "ignore_above": 256 } } },
      "description":         { "type": "text" },
      "visibility":          { "type": "keyword" },
      "status":              { "type": "keyword" },
      "category":            { "type": "keyword" },
      "thumbnail":       	 { "type": "keyword", "index": false },
      "url": 				 { "type": "keyword", "index": false },
      "duration":            { "type": "integer" },
      "qualities":           { "type": "keyword" },
      "tags":                { "type": "keyword" },
      "created_at":          { "type": "date" },
      "updated_at":          { "type": "date" }
    }
  }
}`
