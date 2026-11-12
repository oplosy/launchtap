package postgres

import (
	"context"
	"fmt"
	"math/big"

	"github.com/Contictus/launchtap/backend/internal/stats"
)

// ReadProtocolRevenue sums the ETH credited to the protocol per source and engine in one
// snapshot (ListProtocolRevenueByEngine).
func (r ProtocolReader) ReadProtocolRevenue(ctx context.Context, chainID int64) (stats.Revenue, error) {
	out := newRevenue()
	err := withReadSnapshotBeginner(ctx, r.Pool, chainID, r.DeploymentID, func(ctx context.Context, a *Adapter, s ReadSnapshot) error {
		rows, err := a.queries.ListProtocolRevenueByEngine(ctx, chainID)
		if err != nil {
			return err
		}
		for _, row := range rows {
			values := []struct {
				source *stats.RevenueSource
				text   string
			}{
				{&out.LaunchFees, row.LaunchFees},
				{&out.TradeFees, row.TradeFees},
				{&out.SnipeTax, row.SnipeTax},
				{&out.GraduationRemainder, row.GraduationRemainder},
				{&out.LPFees, row.LpFees},
			}
			for _, v := range values {
				amount, ok := new(big.Int).SetString(v.text, 10)
				if !ok || amount.Sign() < 0 {
					return fmt.Errorf("protocol revenue: invalid amount %q", v.text)
				}
				switch row.EngineVersion {
				case 1:
					v.source.Engine1.Add(v.source.Engine1, amount)
				case 2:
					v.source.Engine2.Add(v.source.Engine2, amount)
				default:
					return fmt.Errorf("protocol revenue: unknown engine version %d", row.EngineVersion)
				}
				v.source.Total.Add(v.source.Total, amount)
				out.Total.Add(out.Total, amount)
			}
		}
		out.Snapshot = s.Identity
		out.Finality = finality(s.State, s.Identity.BlockNumber)
		return nil
	})
	return out, err
}

func newRevenue() stats.Revenue {
	source := func() stats.RevenueSource {
		return stats.RevenueSource{Engine1: new(big.Int), Engine2: new(big.Int), Total: new(big.Int)}
	}
	return stats.Revenue{LaunchFees: source(), TradeFees: source(), SnipeTax: source(), GraduationRemainder: source(), LPFees: source(), Total: new(big.Int)}
}
