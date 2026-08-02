// Command esctl is a manual test client for the media Elasticsearch
// repository. It exercises the same config, client and repository code the API
// server uses, so anything that works here works in the app — and a failure
// here points at the repository layer rather than at an HTTP handler.
//
// It reads .env from the current directory (run it from the repo root) and
// needs the same ELASTICSEARCH_* variables as the server.
//
//	go run ./cmd/esctl index
//	go run ./cmd/esctl create -title "Big Buck Bunny" -creator c1 -tags animation,demo
//	go run ./cmd/esctl update -id <id> -status ready -qualities 480p,720p
//	go run ./cmd/esctl get    -id <id>
//	go run ./cmd/esctl search -q bunny
//	go run ./cmd/esctl delete -id <id>
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"streamflix-backend/config"
	"streamflix-backend/internal/elasticsearch"
	"streamflix-backend/internal/elasticsearch/media"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

// opTimeout bounds every command, so an unreachable cluster fails fast instead
// of hanging the terminal.
const opTimeout = 30 * time.Second

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	args := os.Args[2:]
	var err error
	switch os.Args[1] {
	case "index":
		err = cmdIndex(args)
	case "create":
		err = cmdCreate(args)
	case "update":
		err = cmdUpdate(args)
	case "get":
		err = cmdGet(args)
	case "delete":
		err = cmdDelete(args)
	case "search":
		err = cmdSearch(args)
	case "help", "-h", "--help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `esctl — manual test client for the media Elasticsearch index

Usage:
  go run ./cmd/esctl <command> [flags]

Commands:
  index    create the media index with its explicit mappings (idempotent)
  create   index a new media document
  update   patch an existing document (only the flags you pass are changed)
  get      fetch a document by id
  delete   remove a document by id
  search   full-text search over titles

Note: no update path can clear a field — an unset flag means "leave alone".
Delete and re-create the document to blank one out.

Document flags (create, update):
  -id           document id (generated on create if omitted; required on update)
  -creator      creator id
  -title        title (required on create)
  -description  description
  -visibility   public | unlisted | private   (create default: public)
  -status       uploaded | processing | ready | failed   (create default: uploaded)
  -thumbnail    thumbnail url
  -url          playback url
  -duration     length in seconds
  -qualities    comma-separated rendition labels, e.g. 480p,720p
  -tags         comma-separated tags

Other flags:
  update        -replace  re-index the whole document (Update) instead of
                          patching it (PartialUpdate, the default)
  get, delete   -id <id>
  search        -q <query>

Examples:
  go run ./cmd/esctl index
  go run ./cmd/esctl create -id m1 -title "Big Buck Bunny" -creator c1 -duration 596
  go run ./cmd/esctl update -id m1 -status ready -qualities 480p,720p
  go run ./cmd/esctl get -id m1
  go run ./cmd/esctl search -q bunny
  go run ./cmd/esctl delete -id m1
`)
}

// connect loads config, dials Elasticsearch and verifies the connection before
// returning a repository, mirroring the server's startup handshake. The client
// is returned alongside it for the index-level calls the Repository interface
// does not expose.
func connect(ctx context.Context) (*elasticsearch.Client, media.Repository, string, error) {
	// Not fatal if .env is absent — the variables may come from the shell.
	if err := godotenv.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "note: no .env loaded (%v); using shell environment\n", err)
	}

	cfg, err := config.LoadElasticsearch()
	if err != nil {
		return nil, nil, "", err
	}

	client, err := elasticsearch.NewClient(cfg)
	if err != nil {
		return nil, nil, "", err
	}
	if err := client.Ping(ctx); err != nil {
		return nil, nil, "", fmt.Errorf("unreachable: %w", err)
	}

	return client, media.NewRepository(client, cfg.MediaIndex), cfg.MediaIndex, nil
}

func cmdIndex(args []string) error {
	if err := flag.NewFlagSet("index", flag.ExitOnError).Parse(args); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	client, _, index, err := connect(ctx)
	if err != nil {
		return err
	}

	// Report create-vs-already-there, which EnsureIndex deliberately hides.
	existed, err := indexExists(ctx, client, index)
	if err != nil {
		return err
	}
	if err := media.EnsureIndex(ctx, client, index); err != nil {
		return err
	}

	if existed {
		fmt.Printf("index %q already exists — mappings left untouched\n", index)
	} else {
		fmt.Printf("index %q created\n", index)
	}
	return nil
}

func cmdCreate(args []string) error {
	f := newDocFlags("create")
	if err := f.fs.Parse(args); err != nil {
		return err
	}

	now := time.Now().UTC()
	doc := &media.Document{
		Visibility: media.VisibilityPublic,
		Status:     media.StatusUploaded,
		Qualities:  []string{},
		Tags:       []string{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := f.apply(doc); err != nil {
		return err
	}
	if doc.ID == "" {
		doc.ID = uuid.NewString()
	}
	if doc.Title == "" {
		return errors.New("-title is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	client, repo, index, err := connect(ctx)
	if err != nil {
		return err
	}

	// Indexing into a missing index would let Elasticsearch auto-create it with
	// a dynamic mapping, quietly defeating the strict mapping we rely on. Fail
	// instead and point at the index command.
	existed, err := indexExists(ctx, client, index)
	if err != nil {
		return err
	}
	if !existed {
		return fmt.Errorf("index %q does not exist — run `go run ./cmd/esctl index` first", index)
	}

	if err := repo.Create(ctx, doc); err != nil {
		if errors.Is(err, elasticsearch.ErrAlreadyExists) {
			return fmt.Errorf("id %q already exists in %q — use `update` instead", doc.ID, index)
		}
		return err
	}

	fmt.Printf("created %q in %q\n", doc.ID, index)
	return printDoc(doc)
}

func cmdUpdate(args []string) error {
	f := newDocFlags("update")
	replace := f.fs.Bool("replace", false, "replace the whole document (Update) instead of patching it (PartialUpdate)")
	if err := f.fs.Parse(args); err != nil {
		return err
	}
	if f.id == "" {
		return errors.New("-id is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	_, repo, index, err := connect(ctx)
	if err != nil {
		return err
	}

	if *replace {
		if err := replaceDoc(ctx, repo, index, f); err != nil {
			return err
		}
	} else {
		// PartialUpdate sends only the fields that carry a value, so a document
		// holding just the id plus the passed flags is exactly the change.
		doc := &media.Document{ID: f.id}
		if err := f.apply(doc); err != nil {
			return err
		}
		if err := repo.PartialUpdate(ctx, doc); err != nil {
			if errors.Is(err, elasticsearch.ErrNotFound) {
				return fmt.Errorf("id %q not found in %q", f.id, index)
			}
			return err
		}
	}

	fmt.Printf("updated %q in %q\n", f.id, index)

	// Read back what was actually stored, rather than echoing what we sent —
	// the point of the exercise is seeing the merge Elasticsearch performed.
	stored, err := repo.FindByID(ctx, f.id)
	if err != nil {
		return err
	}
	return printDoc(stored)
}

// replaceDoc drives the full-document Update path: read the current document,
// overlay the passed flags and re-index the result. The read is what keeps
// unset flags from blanking their stored values, and is precisely the
// round trip PartialUpdate avoids.
func replaceDoc(ctx context.Context, repo media.Repository, index string, f *docFlags) error {
	doc, err := repo.FindByID(ctx, f.id)
	if err != nil {
		if errors.Is(err, elasticsearch.ErrNotFound) {
			return fmt.Errorf("id %q not found in %q", f.id, index)
		}
		return err
	}
	if err := f.apply(doc); err != nil {
		return err
	}
	doc.UpdatedAt = time.Now().UTC()

	return repo.Update(ctx, doc)
}

func cmdGet(args []string) error {
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	id := fs.String("id", "", "document id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == "" {
		return errors.New("-id is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	_, repo, index, err := connect(ctx)
	if err != nil {
		return err
	}

	doc, err := repo.FindByID(ctx, *id)
	if err != nil {
		if errors.Is(err, elasticsearch.ErrNotFound) {
			return fmt.Errorf("id %q not found in %q", *id, index)
		}
		return err
	}
	return printDoc(doc)
}

func cmdDelete(args []string) error {
	fs := flag.NewFlagSet("delete", flag.ExitOnError)
	id := fs.String("id", "", "document id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == "" {
		return errors.New("-id is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	_, repo, index, err := connect(ctx)
	if err != nil {
		return err
	}

	if err := repo.Delete(ctx, *id); err != nil {
		if errors.Is(err, elasticsearch.ErrNotFound) {
			return fmt.Errorf("id %q not found in %q", *id, index)
		}
		return err
	}

	fmt.Printf("deleted %q from %q\n", *id, index)
	return nil
}

func cmdSearch(args []string) error {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	query := fs.String("q", "", "title query")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *query == "" {
		return errors.New("-q is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	_, repo, index, err := connect(ctx)
	if err != nil {
		return err
	}

	docs, err := repo.SearchByTitle(ctx, *query)
	if err != nil {
		return err
	}

	fmt.Printf("%d hit(s) for %q in %q\n", len(docs), *query, index)
	if len(docs) == 0 {
		return nil
	}
	return printDoc(docs)
}

// docFlags collects the Document fields settable from the command line. create
// and update share it: create starts from defaults, update starts from the
// stored document, and apply writes only the flags that were actually passed.
type docFlags struct {
	fs *flag.FlagSet

	id          string
	creator     string
	title       string
	description string
	visibility  string
	status      string
	thumbnail   string
	url         string
	duration    int
	qualities   string
	tags        string
}

func newDocFlags(name string) *docFlags {
	f := &docFlags{fs: flag.NewFlagSet(name, flag.ExitOnError)}
	f.fs.StringVar(&f.id, "id", "", "document id")
	f.fs.StringVar(&f.creator, "creator", "", "creator id")
	f.fs.StringVar(&f.title, "title", "", "title")
	f.fs.StringVar(&f.description, "description", "", "description")
	f.fs.StringVar(&f.visibility, "visibility", "", "public | unlisted | private")
	f.fs.StringVar(&f.status, "status", "", "uploaded | processing | ready | failed")
	f.fs.StringVar(&f.thumbnail, "thumbnail", "", "thumbnail url")
	f.fs.StringVar(&f.url, "url", "", "playback url")
	f.fs.IntVar(&f.duration, "duration", 0, "length in seconds")
	f.fs.StringVar(&f.qualities, "qualities", "", "comma-separated rendition labels")
	f.fs.StringVar(&f.tags, "tags", "", "comma-separated tags")
	return f
}

// passed reports whether the named flag appeared on the command line, which is
// how apply tells "set to the zero value" apart from "not mentioned".
func (f *docFlags) passed(name string) bool {
	var found bool
	f.fs.Visit(func(fl *flag.Flag) {
		if fl.Name == name {
			found = true
		}
	})
	return found
}

// apply overwrites doc's fields with the flags that were passed, leaving the
// rest as they were. Timestamps are the callers' responsibility.
func (f *docFlags) apply(doc *media.Document) error {
	if f.passed("id") {
		doc.ID = f.id
	}
	if f.passed("creator") {
		doc.CreatorID = f.creator
	}
	if f.passed("title") {
		doc.Title = f.title
	}
	if f.passed("description") {
		doc.Description = f.description
	}
	if f.passed("visibility") {
		v, err := parseVisibility(f.visibility)
		if err != nil {
			return err
		}
		doc.Visibility = v
	}
	if f.passed("status") {
		s, err := parseStatus(f.status)
		if err != nil {
			return err
		}
		doc.Status = s
	}
	if f.passed("thumbnail") {
		doc.Thumbnail = f.thumbnail
	}
	if f.passed("url") {
		doc.URL = f.url
	}
	if f.passed("duration") {
		doc.Duration = f.duration
	}
	if f.passed("qualities") {
		doc.Qualities = splitList(f.qualities)
	}
	if f.passed("tags") {
		doc.Tags = splitList(f.tags)
	}
	return nil
}

// parseVisibility validates a visibility value. The mapping stores it as a
// keyword, so Elasticsearch would happily accept a typo — catch it here.
func parseVisibility(s string) (media.Visibility, error) {
	switch v := media.Visibility(strings.ToLower(strings.TrimSpace(s))); v {
	case media.VisibilityPublic, media.VisibilityUnlisted, media.VisibilityPrivate:
		return v, nil
	default:
		return "", fmt.Errorf("invalid -visibility %q: want public, unlisted or private", s)
	}
}

// parseStatus validates a status value, for the same reason as
// parseVisibility.
func parseStatus(s string) (media.Status, error) {
	switch v := media.Status(strings.ToLower(strings.TrimSpace(s))); v {
	case media.StatusUploaded, media.StatusProcessing, media.StatusReady, media.StatusFailed:
		return v, nil
	default:
		return "", fmt.Errorf("invalid -status %q: want uploaded, processing, ready or failed", s)
	}
}

// splitList turns "a, b ,, c" into ["a","b","c"]. It always returns a non-nil
// slice so an emptied list is indexed as [] rather than null.
func splitList(s string) []string {
	out := []string{}
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// indexExists reports whether the index is present. The Repository interface
// intentionally exposes only EnsureIndex, so this goes through the raw client.
func indexExists(ctx context.Context, client *elasticsearch.Client, index string) (bool, error) {
	res, err := client.Raw.Indices.Exists(
		[]string{index},
		client.Raw.Indices.Exists.WithContext(ctx),
	)
	if err != nil {
		return false, fmt.Errorf("check index %q: %w", index, err)
	}
	defer res.Body.Close()

	switch res.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("check index %q: unexpected status %s", index, res.Status())
	}
}

// printDoc writes v to stdout as indented JSON, so results can be eyeballed or
// piped into jq.
func printDoc(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
