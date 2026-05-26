package aws

import (
	"fmt"
	"sync"
	"testing"

	ec2_types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// TestNetworkPortsCacheConcurrentAccess is a regression test for issue #129.
// Pre-fix, the cache lookup in getEC2NACLsPerRegion and getEC2SecurityGroupsPerRegion
// read m.nacls / m.securityGroups without holding a mutex while writes from
// other goroutines were locked, producing a "concurrent map read and map write"
// panic under load. Run with `go test -race ./aws/...` to verify the lock fix.
func TestNetworkPortsCacheConcurrentAccess(t *testing.T) {
	m := &NetworkPortsModule{
		nacls:          make(map[string]*[]ec2_types.NetworkAcl),
		securityGroups: make(map[string]*[]ec2_types.SecurityGroup),
	}

	// Pre-populate one region so reader goroutines hit the cached fast-path
	// without needing a live EC2 client.
	cachedNACLs := []ec2_types.NetworkAcl{}
	m.nacls["cached"] = &cachedNACLs
	cachedSGs := []ec2_types.SecurityGroup{}
	m.securityGroups["cached"] = &cachedSGs

	var wg sync.WaitGroup
	const readers = 100
	for i := 0; i < readers; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = m.getEC2NACLsPerRegion("cached")
		}()
		go func() {
			defer wg.Done()
			_ = m.getEC2SecurityGroupsPerRegion("cached")
		}()
	}

	// Writer goroutines populate uncached regions directly through the mutex,
	// mirroring what the live functions do after a successful AWS fetch. The
	// readers above must not race with these writes.
	const writers = 10
	for i := 0; i < writers; i++ {
		region := fmt.Sprintf("uncached-%d", i)
		wg.Add(2)
		go func(r string) {
			defer wg.Done()
			sl := []ec2_types.NetworkAcl{}
			m.naclsMutex.Lock()
			m.nacls[r] = &sl
			m.naclsMutex.Unlock()
		}(region)
		go func(r string) {
			defer wg.Done()
			sl := []ec2_types.SecurityGroup{}
			m.securityGroupsMutex.Lock()
			m.securityGroups[r] = &sl
			m.securityGroupsMutex.Unlock()
		}(region)
	}

	wg.Wait()
}
