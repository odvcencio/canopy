package main

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/pkg/xref"
)

type faninEntry struct {
	Name      string  `json:"name"`
	Signature string  `json:"signature,omitempty"`
	File      string  `json:"file"`
	Kind      string  `json:"kind"`
	StartLine int     `json:"start_line"`
	EndLine   int     `json:"end_line"`
	Incoming  int     `json:"incoming"`
	Outgoing  int     `json:"outgoing"`
	Ratio     float64 `json:"ratio"`
	Generated string  `json:"generated,omitempty"`
}

func newFaninCmd() *cobra.Command {
	var cachePath string
	var noCache bool
	var jsonOutput bool
	var countOnly bool
	var limit int
	var minIncoming int
	var kind string
	var sortBy string

	cmd := &cobra.Command{
		Use:   "fanin [path]",
		Short: "Rank functions by incoming call count (fan-in) to find central code",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sortBy = strings.ToLower(strings.TrimSpace(sortBy))
			switch sortBy {
			case "in", "out", "ratio":
			default:
				return fmt.Errorf("unsupported --sort %q (expected in|out|ratio)", sortBy)
			}

			kind = strings.ToLower(strings.TrimSpace(kind))
			if kind != "" {
				switch kind {
				case "function", "method":
				default:
					return fmt.Errorf("unsupported --kind %q (expected function|method)", kind)
				}
			}

			target := "."
			if len(args) == 1 {
				target = args[0]
			}

			idx, err := loadOrBuild(cmd, cachePath, target, noCache)
			if err != nil {
				return err
			}

			graph, err := xref.Build(idx)
			if err != nil {
				return err
			}

			genMap := generatedFileMap(idx)

			entries := make([]faninEntry, 0, 128)
			for _, def := range graph.Definitions {
				if !def.Callable {
					continue
				}
				if kind != "" {
					switch kind {
					case "function":
						if def.Kind != "function_definition" {
							continue
						}
					case "method":
						if def.Kind != "method_definition" {
							continue
						}
					}
				}

				incoming := graph.IncomingCount(def.ID)
				if incoming < minIncoming {
					continue
				}
				outgoing := graph.OutgoingCount(def.ID)

				ratio := 0.0
				if outgoing > 0 {
					ratio = float64(incoming) / float64(outgoing)
				} else if incoming > 0 {
					ratio = math.Inf(1)
				}

				genTag := ""
				if gi := genMap[def.File]; gi != nil {
					genTag = gi.Generator
				}
				entries = append(entries, faninEntry{
					Name:      def.Name,
					Signature: def.Signature,
					File:      def.File,
					Kind:      def.Kind,
					StartLine: def.StartLine,
					EndLine:   def.EndLine,
					Incoming:  incoming,
					Outgoing:  outgoing,
					Ratio:     ratio,
					Generated: genTag,
				})
			}

			sort.Slice(entries, func(i, j int) bool {
				switch sortBy {
				case "out":
					if entries[i].Outgoing != entries[j].Outgoing {
						return entries[i].Outgoing > entries[j].Outgoing
					}
					return entries[i].Incoming > entries[j].Incoming
				case "ratio":
					ri, rj := entries[i].Ratio, entries[j].Ratio
					infI, infJ := math.IsInf(ri, 1), math.IsInf(rj, 1)
					if infI != infJ {
						return infI
					}
					if ri != rj {
						return ri > rj
					}
					return entries[i].Incoming > entries[j].Incoming
				default: // "in"
					if entries[i].Incoming != entries[j].Incoming {
						return entries[i].Incoming > entries[j].Incoming
					}
					return entries[i].Outgoing > entries[j].Outgoing
				}
			})

			if jsonOutput {
				if countOnly {
					return emitJSON(struct {
						Count int `json:"count"`
					}{
						Count: len(entries),
					})
				}
				if limit > 0 && len(entries) > limit {
					entries = entries[:limit]
				}
				return emitJSON(struct {
					Count   int          `json:"count"`
					SortBy  string       `json:"sort_by"`
					Entries []faninEntry `json:"entries,omitempty"`
				}{
					Count:   len(entries),
					SortBy:  sortBy,
					Entries: entries,
				})
			}

			if countOnly {
				fmt.Println(len(entries))
				return nil
			}

			if limit > 0 && len(entries) > limit {
				entries = entries[:limit]
			}

			for _, e := range entries {
				name := strings.TrimSpace(e.Signature)
				if name == "" {
					name = e.Name
				}
				prefix := ""
				if e.Generated != "" {
					prefix = "[gen] "
				}
				fmt.Printf(
					"%s%s (%s:%d) in=%d out=%d\n",
					prefix,
					name,
					e.File,
					e.StartLine,
					e.Incoming,
					e.Outgoing,
				)
			}
			fmt.Printf("fanin: shown=%d sort=%s\n", len(entries), sortBy)
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().BoolVar(&countOnly, "count", false, "print the number of matching definitions")
	cmd.Flags().IntVar(&limit, "limit", 20, "maximum number of entries to display")
	cmd.Flags().IntVar(&minIncoming, "min", 1, "minimum incoming count to show")
	cmd.Flags().StringVar(&kind, "kind", "", "filter by kind: function or method")
	cmd.Flags().StringVar(&sortBy, "sort", "in", "sort by: in (fan-in), out (fan-out), ratio (in/out)")
	return cmd
}
