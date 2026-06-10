package store

// wouldCreateCycle checks if adding newDeps to taskID would create a cycle.
// edges is a map of task_id -> list of its depends_on_id (dependencies).
// Returns true if:
// - taskID is self-dependent (any newDep == taskID), or
// - taskID is reachable from any newDep following the edges (would create a cycle)
// Otherwise returns false.
func wouldCreateCycle(edges map[string][]string, taskID string, newDeps []string) bool {
	for _, newDep := range newDeps {
		// Self-dependency check
		if newDep == taskID {
			return true
		}
		// Check if taskID is reachable from newDep via DFS
		if isReachable(edges, newDep, taskID) {
			return true
		}
	}
	return false
}

// isReachable checks if target is reachable from source via DFS.
func isReachable(edges map[string][]string, source, target string) bool {
	visited := make(map[string]bool)
	return dfs(edges, source, target, visited)
}

// dfs performs depth-first search from current to target.
func dfs(edges map[string][]string, current, target string, visited map[string]bool) bool {
	if current == target {
		return true
	}
	if visited[current] {
		return false
	}
	visited[current] = true

	for _, neighbor := range edges[current] {
		if dfs(edges, neighbor, target, visited) {
			return true
		}
	}
	return false
}
