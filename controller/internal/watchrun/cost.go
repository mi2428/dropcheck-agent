package watchrun

import (
	"fmt"
	"time"
)

// Cost is a conservative upper bound; failures can skip later acquisitions.
// Loop runs have no finite total, and downloaded response bytes are unbounded.
type Cost struct {
	AttemptsPerRound   uint64
	OperationsPerRound uint64 // Controller dispatches; a cycle contains internal actions.
	OperationsTotal    uint64 // Zero for Loop.
	UnboundedDownload  bool
}

// Cost computes the same attempt limit used at Start, including compound
// acquisitions, prerequisite checks, rotation and owned cleanup operations.
func (p *CompiledPlan) Cost(rounds uint64, loop bool) (Cost, error) {
	if p == nil || len(p.targets) == 0 || len(p.agents) == 0 || loop && rounds != 0 || !loop && rounds == 0 {
		return Cost{}, fmt.Errorf("invalid execution options")
	}
	var cost Cost
	add := func(n uint64) error {
		var err error
		cost.OperationsPerRound, err = checkedSum(cost.OperationsPerRound, n)
		return err
	}
	addAttempts := func(n uint64) error {
		var err error
		cost.AttemptsPerRound, err = checkedSum(cost.AttemptsPerRound, n)
		if err != nil || cost.AttemptsPerRound > maxRetainedAttempts {
			return fmt.Errorf("plan exceeds retained attempt budget")
		}
		return nil
	}
	preflight := map[string]bool{}
	rotation := map[struct{ agent, ssid string }]bool{}
	for _, target := range p.targets {
		agents := uint64(len(target.agents))
		for _, s := range append([]compiledStep{target.connect, target.wait}, target.checks...) {
			if s.id == "" {
				continue
			}
			samples := uint64(1)
			if s.policy.Eventually > 0 && s.policy.StableFor > time.Duration(1<<63-1)-s.policy.Eventually {
				return Cost{}, fmt.Errorf("plan exceeds retained attempt budget")
			}
			if window := s.policy.Eventually + s.policy.StableFor; window > 0 {
				samples += uint64(window/s.policy.Interval) + 1
			}
			attempts, err := checkedProduct(uint64(s.policy.Attempts), uint64(s.policy.Repeat), samples, agents)
			if err != nil {
				return Cost{}, err
			}
			if err := addAttempts(attempts); err != nil {
				return Cost{}, err
			}
			parts, err := checkedProduct(attempts, uint64(len(stepInfo(s).Operations)))
			if err != nil {
				return Cost{}, err
			}
			if err := add(parts); err != nil {
				return Cost{}, err
			}
			if s.operation.Command.GetWget() != nil {
				cost.UnboundedDownload = true
			}
		}
		for _, agent := range target.agents {
			key := agentKey(agent)
			if target.network.Band != "" && target.network.Band != "all" && !preflight[key] {
				preflight[key] = true
				if err := add(1); err != nil {
					return Cost{}, err
				} // One capabilities acquisition per agent.
				if err := addAttempts(1); err != nil {
					return Cost{}, err
				}
			}
			rotationKey := struct{ agent, ssid string }{key, target.network.SSID}
			if target.network.Rotation == "per_round" && !rotation[rotationKey] {
				rotation[rotationKey] = true
				if err := add(2); err != nil {
					return Cost{}, err
				} // Before and after the round.
				if err := addAttempts(2); err != nil {
					return Cost{}, err
				}
			}
			if target.network.Rotation == "per_target" {
				if err := add(1); err != nil {
					return Cost{}, err
				}
				if err := addAttempts(1); err != nil {
					return Cost{}, err
				}
			}
			if target.connect.id != "" {
				if target.network.DisconnectAfter {
					if err := add(1); err != nil {
						return Cost{}, err
					}
				}
				if target.network.ForgetAfter {
					if err := add(1); err != nil {
						return Cost{}, err
					}
				}
			}
		}
	}
	if !loop {
		if rounds > maxRetainedAttempts/max(uint64(1), cost.AttemptsPerRound) {
			return Cost{}, fmt.Errorf("finite rounds exceed retained report budget")
		}
		var err error
		cost.OperationsTotal, err = checkedProduct(cost.OperationsPerRound, rounds)
		if err != nil {
			return Cost{}, err
		}
	}
	return cost, nil
}
