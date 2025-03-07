/*
 * Copyright 2021 CloudWeGo Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package polaris

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/kitex/pkg/discovery"
	"github.com/cloudwego/kitex/pkg/rpcinfo"
	perrors "github.com/pkg/errors"
	"github.com/polarismesh/polaris-go/api"
	"github.com/polarismesh/polaris-go/pkg/log"
	"github.com/polarismesh/polaris-go/pkg/model"
)

const (
	defaultWeight           = 10
	polarisDefaultNamespace = "default"
)

// Resolver is extension interface of Kitex discovery.Resolver.
type Resolver interface {
	discovery.Resolver

	Watcher(ctx context.Context, desc string) (discovery.Change, error)
}

// polarisResolver is a resolver using polaris.
type polarisResolver struct {
	provider api.ProviderAPI
	consumer api.ConsumerAPI
}

// NewPolarisResolver creates a polaris based resolver.
func NewPolarisResolver(endpoints []string) (Resolver, error) {
	sdkCtx, err := GetPolarisConfig(endpoints)
	if err != nil {
		return nil, perrors.WithMessage(err, "create polaris namingClient failed.")
	}

	newInstance := &polarisResolver{
		consumer: api.NewConsumerAPIByContext(sdkCtx),
		provider: api.NewProviderAPIByContext(sdkCtx),
	}

	return newInstance, nil
}

// Target implements the Resolver interface.
func (polaris *polarisResolver) Target(ctx context.Context, target rpcinfo.EndpointInfo) (description string) {
	// serviceName identification is generated by namespace and serviceName to identify serviceName
	var serviceIdentification strings.Builder

	namespace, ok := target.Tag("namespace")
	if ok {
		serviceIdentification.WriteString(namespace)
	} else {
		serviceIdentification.WriteString(polarisDefaultNamespace)
	}
	serviceIdentification.WriteString(":")
	serviceIdentification.WriteString(target.ServiceName())

	return serviceIdentification.String()
}

// Watcher return registered service changes.
func (polaris *polarisResolver) Watcher(ctx context.Context, desc string) (discovery.Change, error) {
	var (
		eps    []discovery.Instance
		add    []discovery.Instance
		update []discovery.Instance
		remove []discovery.Instance
	)
	namespace, serviceName := SplitDescription(desc)
	key := model.ServiceKey{
		Namespace: namespace,
		Service:   serviceName,
	}
	watchReq := api.WatchServiceRequest{}
	watchReq.Key = key
	watchRsp, err := polaris.consumer.WatchService(&watchReq)
	if nil != err {
		log.GetBaseLogger().Fatalf("fail to WatchService, err is %v", err)
	}
	instances := watchRsp.GetAllInstancesResp.Instances

	if nil != instances {
		for _, instance := range instances {
			log.GetBaseLogger().Infof("instance getOneInstance is %s:%d", instance.GetHost(), instance.GetPort())
			eps = append(eps, ChangePolarisInstanceToKitex(instance))
		}
	}

	result := discovery.Result{
		Cacheable: true,
		CacheKey:  desc,
		Instances: eps,
	}
	Change := discovery.Change{}

	select {
	case <-ctx.Done():
		log.GetBaseLogger().Infof("[Polaris resolver] Watch has been finished")
		return Change, nil
	case event := <-watchRsp.EventChannel:
		eType := event.GetSubScribeEventType()
		if eType == api.EventInstance {
			insEvent := event.(*model.InstanceEvent)
			if insEvent.AddEvent != nil {
				for _, instance := range insEvent.AddEvent.Instances {
					add = append(add, ChangePolarisInstanceToKitex(instance))
				}
			}
			if insEvent.UpdateEvent != nil {
				for i := range insEvent.UpdateEvent.UpdateList {
					update = append(update, ChangePolarisInstanceToKitex(insEvent.UpdateEvent.UpdateList[i].After))
				}
			}
			if insEvent.DeleteEvent != nil {
				for _, instance := range insEvent.DeleteEvent.Instances {
					remove = append(remove, ChangePolarisInstanceToKitex(instance))
				}
			}
			Change = discovery.Change{
				Result:  result,
				Added:   add,
				Updated: update,
				Removed: remove,
			}
		}
		return Change, nil
	}
}

// Resolve implements the Resolver interface.
func (polaris *polarisResolver) Resolve(ctx context.Context, desc string) (discovery.Result, error) {
	var eps []discovery.Instance
	namespace, serviceName := SplitDescription(desc)
	getInstances := &api.GetInstancesRequest{}
	getInstances.Namespace = namespace
	getInstances.Service = serviceName
	InstanceResp, err := polaris.consumer.GetInstances(getInstances)
	if nil != err {
		log.GetBaseLogger().Fatalf("fail to getOneInstance, err is %v", err)
	}
	instances := InstanceResp.GetInstances()
	if nil != instances {
		for _, instance := range instances {
			log.GetBaseLogger().Infof("instance getOneInstance is %s:%d", instance.GetHost(), instance.GetPort())
			eps = append(eps, ChangePolarisInstanceToKitex(instance))
		}
	}

	if len(eps) == 0 {
		return discovery.Result{}, fmt.Errorf("no instance remains for %s", desc)
	}
	return discovery.Result{
		Cacheable: true,
		CacheKey:  desc,
		Instances: eps,
	}, nil
}

// Diff implements the Resolver interface.
func (polaris *polarisResolver) Diff(cacheKey string, prev, next discovery.Result) (discovery.Change, bool) {
	return discovery.DefaultDiff(cacheKey, prev, next)
}

// Name implements the Resolver interface.
func (polaris *polarisResolver) Name() string {
	return "Polaris"
}
