package cruzbit

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

type node struct {
	pubkey    string
	ranking   float64
	outbound  float64
}

type edge struct {
	weight float64
	height int64
	time   int64
}

// Graph holds node and edge data.
type Graph struct {
	index map[string]uint32
	nodes map[uint32]*node
	edges map[uint32]map[uint32]*edge
}

// NewGraph initializes and returns a new graph.
func NewGraph() *Graph {
	return &Graph{
		edges: make(map[uint32](map[uint32]*edge)),
		nodes: make(map[uint32]*node),
		index: make(map[string]uint32),
	}
}

// Link creates a weighted edge between a source-target node pair.
// If the edge already exists, the weight is incremented.
func (graph *Graph) Link(src, tgt string, weight float64, height int64, time int64) float64 {
	source := pad44(src)
	target := pad44(tgt)

	if _, ok := graph.index[source]; !ok {
		index := uint32(len(graph.index))
		graph.index[source] = index
		graph.nodes[index] = &node{
			ranking:  0,
			outbound: 0,
			pubkey:   source,
		}
	}

	if _, ok := graph.index[target]; !ok {
		index := uint32(len(graph.index))
		graph.index[target] = index
		graph.nodes[index] = &node{
			ranking:  0,
			outbound: 0,
			pubkey:   target,
		}
	}

	sIndex := graph.index[source]
	tIndex := graph.index[target]

	if _, ok := graph.edges[sIndex]; !ok {
		graph.edges[sIndex] = map[uint32]*edge{}
	}

	if _, ok := graph.edges[sIndex][tIndex]; !ok {
		graph.edges[sIndex][tIndex] = &edge{}
	}
	graph.edges[sIndex][tIndex].weight += weight
	graph.edges[sIndex][tIndex].height = height
	graph.edges[sIndex][tIndex].time = time

	graph.nodes[sIndex].outbound += weight

	return weight
}

func (g *Graph) ToDOT(pubKey string, states map[string]*KeyState) string {

	pkIndex := g.index[pubKey] //defaults to zero- the directory root

	var builder strings.Builder
	builder.WriteString("digraph G {\n")

	includedNodes := []uint32{}

	for from, edge := range g.edges {
		for to, e := range edge {
			if (from == pkIndex || to == pkIndex) && e.weight > 0 {

				builder.WriteString(fmt.Sprintf(
					"  \"%d\" -> \"%d\" [weight=\"%f\", height=\"%d\", time=\"%d\"];\n",
					from, to, e.weight, e.height, e.time,
				))

				if !containsInt(includedNodes, from) {
					includedNodes = append(includedNodes, from)
				}

				if !containsInt(includedNodes, to) {
					includedNodes = append(includedNodes, to)
				}
			}
		}
	}

	// Add nodes with ranks
	for _, id := range includedNodes {
		node := g.nodes[id]
		label := fmt.Sprintf("%.*s", 15, strings.TrimRight(node.pubkey, "0="))
		memo := ""

		if st, ok := states[node.pubkey]; ok {
			memo = st.memo

			if st.label != "" {
				label = st.label
			}

			if st.time != 0 {
				label = label + "/v" + strconv.Itoa(int(st.revision)) + " (" + timeAgo(st.time) + ") "
			}
		}

		if id == 0 {
			label = "root"
		}

		builder.WriteString(fmt.Sprintf(
			"  \"%d\" [label=\"%s\", pubkey=\"%s\", memo=\"%s\", ranking=\"%f\"];\n",
			id, label, node.pubkey, memo, node.ranking,
		))
	}

	builder.WriteString("}\n")
	return builder.String()
}

func containsInt(slice []uint32, value uint32) bool {
	for _, v := range slice {
		if v == value {
			return true
		}
	}
	return false
}

// Checks for relationship to prevent cycles.
func (g *Graph) IsParentDescendant(parent, descendant string) bool {
	parentIndex, pok := g.index[parent]
	descendantIndex, dok := g.index[descendant]

	if !pok || !dok {
		return false
	}

	if parentIndex == 0 || descendantIndex == 0 {
		return false
	}

	visited := make(map[uint32]bool)
	return g.dfs(parentIndex, descendantIndex, visited)
}

func (g *Graph) dfs(current, target uint32, visited map[uint32]bool) bool {
	if current == target {
		return true
	}

	visited[current] = true

	for edge := range g.edges[current] {
		if edge == 0 { // Skip the root node
			continue
		}

		if !visited[edge] {
			if g.dfs(edge, target, visited) {
				return true
			}
		}
	}

	return false
}

// https://github.com/alixaxel/pagerank/blob/master/pagerank.go
// This computes the Rank of every node in the directed graph.
// α (alpha) is the damping factor, usually set to 0.85.
// ε (epsilon) is the convergence criteria, usually set to a tiny value.
//
// This method will run as many iterations as needed, until the graph converges.
func (graph *Graph) Rank(alpha, epsilon float64) {

	normalizedWeights := make(map[uint32](map[uint32]float64))

	Δ := float64(1.0)
	inverse := 1 / float64(len(graph.nodes))

	// Normalize all the edge weights so that their sum amounts to 1.
	for source := range graph.edges {
		if graph.nodes[source].outbound > 0 {
			normalizedWeights[source] = make(map[uint32]float64)
			for target := range graph.edges[source] {
				normalizedWeights[source][target] = graph.edges[source][target].weight / graph.nodes[source].outbound
			}
		}
	}

	for key := range graph.nodes {
		graph.nodes[key].ranking = inverse
	}

	for Δ > epsilon {
		leak := float64(0)
		nodes := map[uint32]float64{}

		for key, value := range graph.nodes {
			nodes[key] = value.ranking

			if value.outbound == 0 {
				leak += value.ranking
			}

			graph.nodes[key].ranking = 0
		}

		leak *= alpha

		for source := range graph.nodes {
			for target, weight := range normalizedWeights[source] {
				graph.nodes[target].ranking += alpha * nodes[source] * weight
			}

			graph.nodes[source].ranking += (1-alpha)*inverse + leak*inverse
		}

		Δ = 0

		for key, value := range graph.nodes {
			Δ += math.Abs(value.ranking - nodes[key])
		}
	}
}

// Reset clears all the current graph data.
func (graph *Graph) Reset() {
	graph.edges = make(map[uint32](map[uint32]*edge))
	graph.nodes = make(map[uint32]*node)
	graph.index = make(map[string]uint32)
}
