/*
Copyright 2024 The HAMi Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package routes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/julienschmidt/httprouter"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"

	"github.com/Project-HAMi/HAMi/pkg/scheduler"
)

func checkBody(w http.ResponseWriter, r *http.Request) {
	if r.Body == nil {
		http.Error(w, "Please send a request body", 400)
		return
	}
}

func PredicateRoute(s *scheduler.Scheduler) httprouter.Handle {
	klog.Infoln("Initializing Predicate Route")
	return func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		klog.Infoln("Entering Predicate Route handler")
		checkBody(w, r)

		var buf bytes.Buffer
		body := io.TeeReader(r.Body, &buf)

		var extenderArgs extenderv1.ExtenderArgs
		var extenderFilterResult *extenderv1.ExtenderFilterResult

		if err := json.NewDecoder(body).Decode(&extenderArgs); err != nil {
			klog.ErrorS(err, "Failed to decode extender arguments")
			extenderFilterResult = &extenderv1.ExtenderFilterResult{
				Error: err.Error(),
			}
		} else {
			extenderFilterResult, err = s.Filter(extenderArgs)
			if err != nil {
				klog.ErrorS(err, "Filter error for pod", "pod", extenderArgs.Pod.Name)
				extenderFilterResult = &extenderv1.ExtenderFilterResult{
					Error: err.Error(),
				}
			}
		}

		if resultBody, err := json.Marshal(extenderFilterResult); err != nil {
			klog.ErrorS(err, "Failed to marshal extender filter result", "result", extenderFilterResult)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(resultBody)
		}
	}
}

func Bind(s *scheduler.Scheduler) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		klog.Infoln("Entering Bind handler")
		var buf bytes.Buffer
		body := io.TeeReader(r.Body, &buf)
		var extenderBindingArgs extenderv1.ExtenderBindingArgs
		var extenderBindingResult *extenderv1.ExtenderBindingResult

		if err := json.NewDecoder(body).Decode(&extenderBindingArgs); err != nil {
			klog.ErrorS(err, "Failed to decode extender binding arguments")
			extenderBindingResult = &extenderv1.ExtenderBindingResult{
				Error: err.Error(),
			}
		} else {
			extenderBindingResult, err = s.Bind(extenderBindingArgs)
			if err != nil {
				klog.ErrorS(err, "Bind error for pod", "pod", extenderBindingArgs.PodName)
				extenderBindingResult = &extenderv1.ExtenderBindingResult{
					Error: err.Error(),
				}
			}
		}

		if response, err := json.Marshal(extenderBindingResult); err != nil {
			klog.ErrorS(err, "Failed to marshal binding result", "result", extenderBindingResult)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			errMsg := fmt.Sprintf("{'error':'%s'}", err.Error())
			w.Write([]byte(errMsg))
		} else {
			klog.V(5).InfoS("Returning bind response", "result", extenderBindingResult)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(response)
		}
	}
}

func bind(args extenderv1.ExtenderBindingArgs, bindFunc func(string, string, types.UID, string) error) *extenderv1.ExtenderBindingResult {
	err := bindFunc(args.PodName, args.PodNamespace, args.PodUID, args.Node)
	errMsg := ""
	if err != nil {
		klog.ErrorS(err, "Bind error", "pod", args.PodName, "namespace", args.PodNamespace, "node", args.Node, "uid", args.PodUID)
		errMsg = err.Error()
	}
	return &extenderv1.ExtenderBindingResult{
		Error: errMsg,
	}
}

func WebHookRoute() httprouter.Handle {
	h, err := scheduler.NewWebHook()
	if err != nil {
		klog.ErrorS(err, "Failed to create new webhook")
	}
	return func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		klog.Infof("Handling webhook request on %s", r.URL.Path)
		h.ServeHTTP(w, r)
	}
}

func HealthzRoute() httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		klog.Infoln("Health check endpoint hit")
		w.WriteHeader(http.StatusOK)
	}
}
