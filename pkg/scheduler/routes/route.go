/*
 * Copyright © 2021 peizhaoyou <peizhaoyou@4paradigm.com>
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

package routes

import (
    "4pd.io/k8s-vgpu/pkg/scheduler"
    "bytes"
    "encoding/json"
    "io"
    "net/http"

    "github.com/julienschmidt/httprouter"
    "k8s.io/klog/v2"
    extenderv1 "k8s.io/kube-scheduler/extender/v1"
)

func checkBody(w http.ResponseWriter, r *http.Request) {
    if r.Body == nil {
        http.Error(w, "Please send a request body", 400)
        return
    }
}

func PredicateRoute(s *scheduler.Scheduler) httprouter.Handle {
    return func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
        checkBody(w, r)

        var buf bytes.Buffer
        body := io.TeeReader(r.Body, &buf)

        var extenderArgs extenderv1.ExtenderArgs
        var extenderFilterResult *extenderv1.ExtenderFilterResult

        if err := json.NewDecoder(body).Decode(&extenderArgs); err != nil {
            extenderFilterResult = &extenderv1.ExtenderFilterResult{
                Error: err.Error(),
            }
        } else {
            extenderFilterResult, err = s.Filter(extenderArgs)
            if err != nil {
                klog.Errorf("pod %v filter error, %v", extenderArgs.Pod.Name, err)
                extenderFilterResult = &extenderv1.ExtenderFilterResult{
                    Error: err.Error(),
                }
            }
        }

        if resultBody, err := json.Marshal(extenderFilterResult); err != nil {
            klog.Errorf("Failed to marshal extenderFilterResult: %+v, %+v",
                err, extenderFilterResult)
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

func WebHookRoute() httprouter.Handle {
    h, err := scheduler.NewWebHook()
    if err != nil {
        klog.Fatalf("new web hook error, %v", err)
    }
    return func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
        h.ServeHTTP(w, r)
    }
}
