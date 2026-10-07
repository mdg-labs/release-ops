package source

func pickNewestRelease(candidates []Release) *Release {
	if len(candidates) == 0 {
		return nil
	}

	best := &candidates[0]
	for i := 1; i < len(candidates); i++ {
		candidate := &candidates[i]
		if candidate.PublishedAt.After(best.PublishedAt) {
			best = candidate
			continue
		}
		if candidate.PublishedAt.Equal(best.PublishedAt) && candidate.Tag > best.Tag {
			best = candidate
		}
	}
	return best
}
