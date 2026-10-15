package cli

import (
	"time"

	"github.com/laurentL/salt-api-cli/internal/saltclient"
	"github.com/spf13/cobra"
)

// TargetFlags holds Salt target matching and execution mode flags.
type TargetFlags struct {
	TgtType      string
	Grain        bool
	GrainPCRE    bool
	PCRE         bool
	List         bool
	Pillar       bool
	PillarPCRE   bool
	Compound     bool
	Nodegroup    bool
	IPCIDR       bool
	Batch        string
	Async        bool
	Wait         bool
	WaitInterval time.Duration
	RawArgs      bool
}

// AddTargetFlags registers Salt target and execution flags onto a Cobra command.
func (tf *TargetFlags) AddTargetFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVar(&tf.TgtType, "tgt-type", "", "Target matching type (glob, pcre, list, grain, grain_pcre, pillar, pillar_pcre, nodegroup, compound, ipcidr)")
	f.BoolVarP(&tf.Grain, "grain", "G", false, "Match targets by Salt grains (tgt_type=grain)")
	f.BoolVar(&tf.GrainPCRE, "grain-pcre", false, "Match targets by Salt grains regex (tgt_type=grain_pcre)")
	f.BoolVarP(&tf.PCRE, "pcre", "E", false, "Match targets by PCRE regular expression (tgt_type=pcre)")
	f.BoolVarP(&tf.List, "list", "L", false, "Match targets as comma-separated minion list (tgt_type=list)")
	f.BoolVarP(&tf.Pillar, "pillar", "I", false, "Match targets by Salt pillar data (tgt_type=pillar)")
	f.BoolVar(&tf.PillarPCRE, "pillar-pcre", false, "Match targets by Salt pillar regex (tgt_type=pillar_pcre)")
	f.BoolVarP(&tf.Compound, "compound", "C", false, "Match targets by compound expression (tgt_type=compound)")
	f.BoolVarP(&tf.Nodegroup, "nodegroup", "N", false, "Match targets by Salt master nodegroup (tgt_type=nodegroup)")
	f.BoolVarP(&tf.IPCIDR, "ipcidr", "S", false, "Match targets by subnet/IPv4 address (tgt_type=ipcidr)")
	f.StringVarP(&tf.Batch, "batch", "b", "", "Execute in batches of specified size or percentage (e.g. '10%' or '5')")
	f.BoolVar(&tf.Async, "async", false, "Start execution asynchronously (local_async) and return JID")
	f.BoolVarP(&tf.Wait, "wait", "w", false, "When running asynchronously, poll /jobs/<jid> until all minions return")
	f.DurationVar(&tf.WaitInterval, "wait-interval", 1500*time.Millisecond, "Polling interval when --wait is enabled")
	f.BoolVar(&tf.RawArgs, "raw-args", false, "Pass all positional and keyword arguments as raw strings without type coercion")
}

// ResolveTargetType resolves the selected saltclient.TargetType from flags.
func (tf *TargetFlags) ResolveTargetType() saltclient.TargetType {
	if tf.TgtType != "" {
		return saltclient.TargetType(tf.TgtType)
	}
	switch {
	case tf.Grain:
		return saltclient.TargetGrain
	case tf.GrainPCRE:
		return saltclient.TargetGrainPCRE
	case tf.PCRE:
		return saltclient.TargetPCRE
	case tf.List:
		return saltclient.TargetList
	case tf.Pillar:
		return saltclient.TargetPillar
	case tf.PillarPCRE:
		return saltclient.TargetPillarPCRE
	case tf.Compound:
		return saltclient.TargetCompound
	case tf.Nodegroup:
		return saltclient.TargetNodegroup
	case tf.IPCIDR:
		return saltclient.TargetIPCIDR
	default:
		return saltclient.TargetGlob
	}
}
