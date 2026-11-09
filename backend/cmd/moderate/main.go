// Command moderate is the operator CLI for token comments: it lists open reports, hides and
// unhides comments, bans and unbans authors and resolves reports. It writes straight to
// PostgreSQL; there is no HTTP admin surface, so the DATABASE_URL credential is the access
// control. Every mutation runs in one transaction that also appends a moderation_actions row
// and sends the `comment` refresh hint for each affected token.
//
//	moderate <command> -chain-id N -deployment-id ID -operator NAME [flags]
//
// Commands: reports, hide, unhide, ban, unban, resolve. Unknown targets exit non-zero and
// write nothing.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/Contictus/launchtap/backend/internal/config"
	"github.com/Contictus/launchtap/backend/internal/redact"
	"github.com/Contictus/launchtap/backend/internal/social"
	storepostgres "github.com/Contictus/launchtap/backend/internal/store/postgres"
	"github.com/ethereum/go-ethereum/common"
)

const (
	defaultReportLimit = 50
	maxReportLimit     = 500
	maxDIDBytes        = 255
)

// command is a fully validated invocation.
type command struct {
	Name       string
	Scope      social.Scope
	Operator   string
	Limit      int
	CommentID  int64
	DID        string
	Reason     string
	Resolution social.Resolution
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := execute(ctx, os.Args[1:], os.Getenv, os.Stdout)
	stop()
	if err != nil {
		slog.Error("moderate failed", "error", redact.Error(err, os.Getenv("DATABASE_URL")))
		os.Exit(1)
	}
}

func execute(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	cmd, err := parseCommand(args)
	if err != nil {
		return err
	}
	c, err := config.LoadDatabase(getenv)
	if err != nil {
		return err
	}
	pool, err := storepostgres.OpenPool(ctx, c.DatabaseURL, storepostgres.PoolOptions{MaxConns: 2})
	if err != nil {
		return fmt.Errorf("open database pool: %w", err)
	}
	defer pool.Close()
	return run(ctx, cmd, storepostgres.ModerationStore{Pool: pool}, out)
}

// parseCommand validates every argument without touching configuration or the database.
func parseCommand(args []string) (command, error) {
	if len(args) == 0 {
		return command{}, errors.New("usage: moderate <reports|hide|unhide|ban|unban|resolve> -chain-id N -deployment-id ID -operator NAME [flags]")
	}
	cmd := command{Name: args[0]}
	set := flag.NewFlagSet("moderate "+cmd.Name, flag.ContinueOnError)
	set.SetOutput(io.Discard)
	var chainID int64
	var deploymentID, resolution string
	set.Int64Var(&chainID, "chain-id", 0, "chain id of the deployment (required)")
	set.StringVar(&deploymentID, "deployment-id", "", "deployment id (required)")
	set.StringVar(&cmd.Operator, "operator", "", "operator name recorded in the audit log (required, 1-64 bytes)")
	switch cmd.Name {
	case "reports":
		set.IntVar(&cmd.Limit, "limit", defaultReportLimit, "maximum comments to list")
	case "hide", "unhide":
		set.Int64Var(&cmd.CommentID, "comment", 0, "comment id")
		set.StringVar(&cmd.Reason, "reason", "", "reason (1-500 bytes)")
	case "ban":
		set.StringVar(&cmd.DID, "did", "", "Privy DID of the user")
		set.Int64Var(&cmd.CommentID, "comment", 0, "ban the author of this comment")
		set.StringVar(&cmd.Reason, "reason", "", "reason (1-500 bytes)")
	case "unban":
		set.StringVar(&cmd.DID, "did", "", "Privy DID of the user")
		set.StringVar(&cmd.Reason, "reason", "", "reason (1-500 bytes)")
	case "resolve":
		set.Int64Var(&cmd.CommentID, "comment", 0, "comment id")
		set.StringVar(&resolution, "resolution", "", "dismissed or actioned")
		set.StringVar(&cmd.Reason, "reason", "", "reason (1-500 bytes)")
	default:
		return command{}, fmt.Errorf("unknown command %q", cmd.Name)
	}
	if err := set.Parse(args[1:]); err != nil {
		return command{}, fmt.Errorf("parse flags: %w", err)
	}
	if set.NArg() != 0 {
		return command{}, fmt.Errorf("unexpected arguments: %v", set.Args())
	}
	if chainID < 1 {
		return command{}, errors.New("-chain-id is required and must be positive")
	}
	if strings.TrimSpace(deploymentID) == "" {
		return command{}, errors.New("-deployment-id is required")
	}
	cmd.Scope = social.Scope{ChainID: chainID, DeploymentID: deploymentID}
	if n := len(cmd.Operator); n < 1 || n > social.MaxOperatorBytes {
		return command{}, fmt.Errorf("-operator is required and must be 1-%d bytes", social.MaxOperatorBytes)
	}

	if cmd.Name == "reports" {
		if cmd.Limit < 1 || cmd.Limit > maxReportLimit {
			return command{}, fmt.Errorf("-limit must be between 1 and %d", maxReportLimit)
		}
		return cmd, nil
	}
	if n := len(cmd.Reason); n < 1 || n > social.MaxModerationReasonBytes {
		return command{}, fmt.Errorf("-reason is required and must be 1-%d bytes", social.MaxModerationReasonBytes)
	}
	switch cmd.Name {
	case "ban":
		if (cmd.DID == "") == (cmd.CommentID == 0) {
			return command{}, errors.New("ban requires exactly one of -did and -comment")
		}
	case "unban":
		if cmd.DID == "" {
			return command{}, errors.New("-did is required")
		}
	default:
		if cmd.CommentID == 0 {
			return command{}, errors.New("-comment is required")
		}
	}
	if cmd.DID != "" && len(cmd.DID) > maxDIDBytes {
		return command{}, fmt.Errorf("-did must be at most %d bytes", maxDIDBytes)
	}
	if cmd.CommentID < 0 {
		return command{}, errors.New("-comment must be positive")
	}
	if cmd.Name == "resolve" {
		r, err := social.ParseResolution(resolution)
		if err != nil {
			return command{}, fmt.Errorf("-resolution: %w", err)
		}
		cmd.Resolution = r
	}
	return cmd, nil
}

// run executes one validated command and prints a one-line result (or the report listing).
// Nothing is printed when the action fails.
func run(ctx context.Context, cmd command, mod social.Moderator, out io.Writer) error {
	s, op, reason := cmd.Scope, cmd.Operator, cmd.Reason
	switch cmd.Name {
	case "reports":
		reports, err := mod.OpenReports(ctx, s, cmd.Limit)
		if err != nil {
			return err
		}
		return printReports(out, reports)
	case "hide":
		if err := mod.Hide(ctx, s, cmd.CommentID, op, reason); err != nil {
			return err
		}
		_, err := fmt.Fprintf(out, "hid comment %d\n", cmd.CommentID)
		return err
	case "unhide":
		if err := mod.Unhide(ctx, s, cmd.CommentID, op, reason); err != nil {
			return err
		}
		_, err := fmt.Fprintf(out, "unhid comment %d\n", cmd.CommentID)
		return err
	case "ban":
		var tokens []common.Address
		var err error
		if cmd.DID != "" {
			tokens, err = mod.Ban(ctx, s, cmd.DID, op, reason)
		} else {
			tokens, err = mod.BanCommentAuthor(ctx, s, cmd.CommentID, op, reason)
		}
		if err != nil {
			return err
		}
		return printTokens(out, "banned author", tokens)
	case "unban":
		tokens, err := mod.Unban(ctx, s, cmd.DID, op, reason)
		if err != nil {
			return err
		}
		return printTokens(out, "unbanned author", tokens)
	case "resolve":
		n, err := mod.Resolve(ctx, s, cmd.CommentID, cmd.Resolution, op, reason)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "resolved %d report(s) of comment %d as %s\n", n, cmd.CommentID, cmd.Resolution)
		return err
	}
	return fmt.Errorf("unknown command %q", cmd.Name)
}

func printTokens(out io.Writer, what string, tokens []common.Address) error {
	list := make([]string, len(tokens))
	for i, t := range tokens {
		list[i] = t.Hex()
	}
	_, err := fmt.Fprintf(out, "%s; %d token(s) affected: %s\n", what, len(tokens), strings.Join(list, ","))
	return err
}

// printReports writes one tab-separated line per comment. The body is quoted so a comment cannot
// inject terminal control sequences or extra lines.
func printReports(out io.Writer, reports []social.ReportSummary) error {
	if len(reports) == 0 {
		_, err := fmt.Fprintln(out, "no open reports")
		return err
	}
	for _, r := range reports {
		reasons := make([]string, len(r.Reasons))
		for i, reason := range r.Reasons {
			reasons[i] = string(reason)
		}
		_, err := fmt.Fprintf(out, "comment=%d\ttoken=%s\tcreated_at=%s\treports=%d\treasons=%s\tauthor_did=%s\twallet=%s\tbody=%s\n",
			r.CommentID, r.Token.Hex(), r.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"), r.Reports,
			strings.Join(reasons, ","), strconv.QuoteToASCII(r.AuthorDID), r.Wallet.Hex(), strconv.Quote(r.Body))
		if err != nil {
			return err
		}
	}
	return nil
}
