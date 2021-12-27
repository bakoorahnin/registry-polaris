// Copyright 2021 CloudWeGo authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// package Polaris resolver
package polaris

import (
	"context"
	"fmt"
	"log"
	"strconv"

	"github.com/cloudwego/kitex/pkg/discovery"
	"github.com/cloudwego/kitex/pkg/rpcinfo"
	perrors "github.com/pkg/errors"
	"github.com/polarismesh/polaris-go/api"
	"github.com/polarismesh/polaris-go/pkg/model"
	"sync"
	"time"
)

const (
	defaultWeight           = 10
	PolarisDefaultNamespace = "default"
)

// instanceInfo used to stored service basic info in polaris.
type instanceInfo struct {
	Network string            `json:"network"`
	Address string            `json:"address"`
	Weight  int               `json:"weight"`
	Tags    map[string]string `json:"tags"`
}

// Registry
type Resolver interface {

	discovery.Resolver

	doHeartbeat(ctx context.Context, ins *api.InstanceRegisterRequest)
}

// PolarisResolver is a resolver using Polaris.
type PolarisResolver struct {
	namespace    string
	provider     api.ProviderAPI
	consumer     api.ConsumerAPI
	services     *sync.Map
	instanceLock *sync.RWMutex
}

// NewPolarisResolver creates a Polaris based resolver.
func NewPolarisResolver(endpoints []string) (Resolver, error) {
	return NewPolarisResolverWithAuth(endpoints, "", "")
}

// NewPolarisResolverWithAuth creates a Polaris based resolver with given username and password.
func NewPolarisResolverWithAuth(endpoints []string, username, password string) (Resolver, error) {

	sdkCtx, err := GetPolarisConfig(endpoints)

	if err != nil {
		return nil, perrors.WithMessage(err, "create polaris namingClient failed.")
	}

	newInstance := &PolarisResolver{
		namespace:    PolarisDefaultNamespace,
		instanceLock: &sync.RWMutex{},
		consumer:     api.NewConsumerAPIByContext(sdkCtx),
		provider:     api.NewProviderAPIByContext(sdkCtx),
		services:     new(sync.Map),
	}

	return newInstance, nil
}

// Target implements the Resolver interface.
func (polaris *PolarisResolver) Target(ctx context.Context, target rpcinfo.EndpointInfo) (description string) {
	return target.ServiceName()
}

// Resolve implements the Resolver interface.
func (polaris *PolarisResolver) Resolve(ctx context.Context, desc string) (discovery.Result, error) {
	var (
		info instanceInfo
		eps  []discovery.Instance
	)
	getOneRequest := &api.GetInstancesRequest{}
	getOneRequest.Namespace = PolarisDefaultNamespace
	getOneRequest.Service = desc
	oneInstResp, err := polaris.consumer.GetInstances(getOneRequest)
	if nil != err {
		log.Fatalf("fail to getOneInstance, err is %v", err)
	}
	instances := oneInstResp.GetInstances()
	if nil != instances {
		for _, instance := range instances {
			log.Printf("instance getOneInstance is %s:%d", instance.GetHost(), instance.GetPort())
			weight := instance.GetWeight()
			if weight <= 0 {
				weight = defaultWeight
			}
			addr := instance.GetHost() + ":" + strconv.Itoa(int(instance.GetPort()))
			eps = append(eps, discovery.NewInstance(instance.GetProtocol(), addr, weight, info.Tags))
		}
	}

	if len(eps) == 0 {
		return discovery.Result{}, fmt.Errorf("no instance remains for %v", desc)
	}
	return discovery.Result{
		Cacheable: true,
		CacheKey:  desc,
		Instances: eps,
	}, nil
}

// Diff implements the Resolver interface.
func (polaris *PolarisResolver) Diff(cacheKey string, prev, next discovery.Result) (discovery.Change, bool) {
	return discovery.DefaultDiff(cacheKey, prev, next)
}

// Name implements the Resolver interface.
func (polaris *PolarisResolver) Name() string {
	return "Polaris"
}

// doHeartbeat
func (polaris *PolarisResolver) doHeartbeat(ctx context.Context, ins *api.InstanceRegisterRequest) {

	ticker := time.NewTicker(time.Duration(4) * time.Second)

	heartbeat := &api.InstanceHeartbeatRequest{
		InstanceHeartbeatRequest: model.InstanceHeartbeatRequest{
			Service:   ins.Service,
			Namespace: ins.Namespace,
			Host:      ins.Host,
			Port:      ins.Port,
		},
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			polaris.provider.Heartbeat(heartbeat)
		}
	}
}
