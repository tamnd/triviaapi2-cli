package triviaapi2

import (
	"context"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes The Trivia API v2 as a kit Domain: a driver that a
// multi-domain host (ant) enables with a single blank import,
//
//	import _ "github.com/tamnd/triviaapi2-cli/triviaapi2"
//
// exactly as a database/sql program enables a driver with `import _
// "github.com/lib/pq"`. The init below registers it; the host then
// dereferences triviaapi2:// URIs by routing to the operations Register
// installs. The same Domain also builds the standalone triviaapi2 binary
// (see cli.NewApp), so the binary and a host share one source of truth.
func init() { kit.Register(Domain{}) }

// Domain is the triviaapi2 driver. It carries no state; the per-run client is
// built by the factory Register hands kit.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against,
// and the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "triviaapi2",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "triviaapi2",
			Short:  "Fetch trivia questions and categories from The Trivia API v2",
			Long: `triviaapi2 reads public trivia data from the-trivia-api.com/v2 over HTTPS,
shapes it into clean records, and prints output that pipes into the rest
of your tools. No API key needed.`,
			Site: Host,
			Repo: "https://github.com/tamnd/triviaapi2-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	kit.Handle(app, kit.OpMeta{
		Name: "questions", Group: "read", List: true,
		Summary: "Get trivia questions (--category, --difficulty, --limit)",
	}, listQuestions)

	kit.Handle(app, kit.OpMeta{
		Name: "categories", Group: "read", List: true,
		Summary: "List all trivia categories",
	}, listCategories)

	kit.Handle(app, kit.OpMeta{
		Name: "random", Group: "read", Single: true,
		Summary: "Get a single random trivia question",
	}, randomQuestion)
}

// newClient builds the client from the host-resolved config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := NewClient()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.HTTP.Timeout = cfg.Timeout
	}
	return c, nil
}

// --- inputs ---

type questionsInput struct {
	Category   string  `kit:"flag" help:"category name (Science, Geography, etc)"`
	Difficulty string  `kit:"flag" help:"difficulty (easy, medium, hard)"`
	Limit      int     `kit:"flag,inherit" help:"max questions (max 50)"`
	Client     *Client `kit:"inject"`
}

type categoriesInput struct {
	Client *Client `kit:"inject"`
}

type randomInput struct {
	Difficulty string  `kit:"flag" help:"difficulty (easy, medium, hard)"`
	Client     *Client `kit:"inject"`
}

// --- handlers ---

func listQuestions(ctx context.Context, in questionsInput, emit func(*Question) error) error {
	cats := joinCategories(in.Category)
	diff := strings.TrimSpace(in.Difficulty)
	qs, err := in.Client.GetQuestions(ctx, in.Limit, cats, diff)
	if err != nil {
		return mapErr(err)
	}
	for _, q := range qs {
		if err := emit(q); err != nil {
			return err
		}
	}
	return nil
}

func listCategories(ctx context.Context, in categoriesInput, emit func(*CategoryInfo) error) error {
	cats, err := in.Client.GetCategories(ctx)
	if err != nil {
		return mapErr(err)
	}
	for _, c := range cats {
		if err := emit(c); err != nil {
			return err
		}
	}
	return nil
}

func randomQuestion(ctx context.Context, in randomInput, emit func(*Question) error) error {
	diff := strings.TrimSpace(in.Difficulty)
	qs, err := in.Client.GetQuestions(ctx, 1, "", diff)
	if err != nil {
		return mapErr(err)
	}
	if len(qs) == 0 {
		return errs.NotFound("no question returned")
	}
	return emit(qs[0])
}

// --- Resolver: the URI-native string functions, pure and network-free ---

// Classify is not meaningful for a pure-API service with no page-like URIs.
// Return an error so `ant resolve` gracefully reports unsupported input.
func (Domain) Classify(input string) (uriType, id string, err error) {
	return "", "", errs.Usage("triviaapi2 URIs are not addressable by reference: %q", input)
}

// Locate is the inverse of Classify; not applicable for this domain.
func (Domain) Locate(uriType, id string) (string, error) {
	return "", errs.Usage("triviaapi2 has no resource type %q", uriType)
}

// --- helpers ---

func mapErr(err error) error {
	return err
}
