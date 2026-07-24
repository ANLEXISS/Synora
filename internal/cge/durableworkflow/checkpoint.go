package durableworkflow

func (c *Coordinator) StateDigest() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state.Digest
}
