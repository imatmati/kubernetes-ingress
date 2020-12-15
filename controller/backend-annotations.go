// Copyright 2019 HAProxy Technologies LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"strconv"
	"strings"

	"github.com/haproxytech/kubernetes-ingress/controller/haproxy"
	"github.com/haproxytech/kubernetes-ingress/controller/utils"
	"github.com/haproxytech/models/v2"
)

func (c *HAProxyController) handleSSLPassthrough(ingress *Ingress, service *Service, path *IngressPath, backend *models.Backend, newBackend bool) (updateBackendSwitching bool) {

	if path.IsTCPService || path.IsDefaultBackend {
		return false
	}
	updateBackendSwitching = false
	annSSLPassthrough, _ := GetValueFromAnnotations("ssl-passthrough", service.Annotations, ingress.Annotations, c.cfg.ConfigMap.Annotations)
	status := annSSLPassthrough.Status
	if status == EMPTY {
		status = path.Status
	}
	if status != EMPTY || newBackend {
		enabled, err := utils.GetBoolValue(annSSLPassthrough.Value, "ssl-passthrough")
		if err != nil {
			c.Logger.Errorf("ssl-passthrough annotation: %s", err)
			return updateBackendSwitching
		}
		if enabled {
			if !path.IsSSLPassthrough {
				path.IsSSLPassthrough = true
				backend.Mode = "tcp"
				updateBackendSwitching = true

			}
		} else if path.IsSSLPassthrough {
			path.IsSSLPassthrough = false
			backend.Mode = "http"
			updateBackendSwitching = true
		}
	}
	return updateBackendSwitching
}

// Update backend with annotations values.
func (c *HAProxyController) handleBackendAnnotations(ingress *Ingress, service *Service, backendModel *models.Backend, newBackend bool) (activeAnnotations bool) {
	activeAnnotations = false
	backend := haproxy.Backend(*backendModel)
	backendAnnotations := make(map[string]*StringW, 6)

	backendAnnotations["abortonclose"], _ = GetValueFromAnnotations("abortonclose", service.Annotations, ingress.Annotations, c.cfg.ConfigMap.Annotations)
	backendAnnotations["cookie-persistence"], _ = GetValueFromAnnotations("cookie-persistence", service.Annotations, ingress.Annotations, c.cfg.ConfigMap.Annotations)
	backendAnnotations["load-balance"], _ = GetValueFromAnnotations("load-balance", service.Annotations, ingress.Annotations, c.cfg.ConfigMap.Annotations)
	backendAnnotations["timeout-check"], _ = GetValueFromAnnotations("timeout-check", service.Annotations, ingress.Annotations, c.cfg.ConfigMap.Annotations)
	if backend.Mode == "http" {
		backendAnnotations["check-http"], _ = GetValueFromAnnotations("check-http", service.Annotations, ingress.Annotations, c.cfg.ConfigMap.Annotations)
		backendAnnotations["forwarded-for"], _ = GetValueFromAnnotations("forwarded-for", service.Annotations, ingress.Annotations, c.cfg.ConfigMap.Annotations)
	}

	// The DELETED status of an annotation is handled explicitly
	// only when there is no default annotation value.
	for k, v := range backendAnnotations {
		if v == nil {
			continue
		}
		if v.Status != EMPTY || newBackend {
			c.Logger.Debugf("Backend '%s': Configuring '%s' annotation", backend.Name, k)
			switch k {
			case "abortonclose":
				if err := backend.UpdateAbortOnClose(v.Value); err != nil {
					c.Logger.Error(err)
					continue
				}
				activeAnnotations = true
			case "check-http":
				if v.Status == DELETED && !newBackend {
					backend.Httpchk = nil
				} else if err := backend.UpdateHttpchk(v.Value); err != nil {
					c.Logger.Errorf("%s annotation: %s", k, err)
					continue
				}
				activeAnnotations = true
			case "cookie-persistence":
				if v.Status == DELETED && !newBackend {
					backend.Cookie = nil
				} else {
					cookie := c.handleCookieAnnotations(ingress, service)
					if err := backend.UpdateCookie(&cookie); err != nil {
						c.Logger.Errorf("%s annotation: %s", k, err)
						continue
					}
				}
				activeAnnotations = true
			case "forwarded-for":
				if err := backend.UpdateForwardfor(v.Value); err != nil {
					c.Logger.Errorf("%s annotation: %s", k, err)
					continue
				}
				activeAnnotations = true
			case "load-balance":
				if err := backend.UpdateBalance(v.Value); err != nil {
					c.Logger.Errorf("%s annotation: %s", k, err)
					continue
				}
				activeAnnotations = true
			}
		}
	}
	*backendModel = models.Backend(backend)
	return activeAnnotations

}

// Update server with annotations values.
func (c *HAProxyController) handleServerAnnotations(serverModel *models.Server, annotations map[string]*StringW) {
	server := haproxy.Server(*serverModel)

	// The DELETED status of an annotation is handled explicitly
	// only when there is no default annotation value.
	for k, v := range annotations {
		if v == nil {
			continue
		}
		c.Logger.Tracef("Server '%s': Configuring '%s' annotation", server.Name, k)
		switch k {
		case "cookie-persistence":
			if v.Status == DELETED {
				server.Cookie = ""
			} else {
				server.Cookie = v.Value
			}
		case "check":
			if err := server.UpdateCheck(v.Value); err != nil {
				c.Logger.Errorf("%s annotation: %s", k, err)
				continue
			}
		case "check-interval":
			if v.Status == DELETED {
				server.Inter = nil
			} else if err := server.UpdateInter(v.Value); err != nil {
				c.Logger.Errorf("%s annotation: %s", k, err)
				continue
			}
		case "pod-maxconn":
			if v.Status == DELETED {
				server.Maxconn = nil
			} else if err := server.UpdateMaxconn(v.Value); err != nil {
				c.Logger.Errorf("%s annotation: %s", k, err)
				continue
			}
		case "server-ssl":
			if err := server.UpdateServerSsl(v.Value); err != nil {
				c.Logger.Errorf("%s annotation: %s", k, err)
				continue
			}
		}
	}
	*serverModel = models.Server(server)
}

func (c *HAProxyController) handleCookieAnnotations(ingress *Ingress, service *Service) models.Cookie {

	cookieAnnotations := make(map[string]*StringW, 11)
	cookieAnnotations["cookie-persistence"], _ = GetValueFromAnnotations("cookie-persistence", service.Annotations, ingress.Annotations, c.cfg.ConfigMap.Annotations)
	cookieAnnotations["cookie-domain"], _ = GetValueFromAnnotations("cookie-domain", service.Annotations, ingress.Annotations, c.cfg.ConfigMap.Annotations)
	cookieAnnotations["cookie-dynamic"], _ = GetValueFromAnnotations("cookie-dynamic", service.Annotations, ingress.Annotations, c.cfg.ConfigMap.Annotations)
	cookieAnnotations["cookie-httponly"], _ = GetValueFromAnnotations("cookie-httponly", service.Annotations, ingress.Annotations, c.cfg.ConfigMap.Annotations)
	cookieAnnotations["cookie-indirect"], _ = GetValueFromAnnotations("cookie-indirect", service.Annotations, ingress.Annotations, c.cfg.ConfigMap.Annotations)
	cookieAnnotations["cookie-maxidle"], _ = GetValueFromAnnotations("cookie-maxidle", service.Annotations, ingress.Annotations, c.cfg.ConfigMap.Annotations)
	cookieAnnotations["cookie-maxlife"], _ = GetValueFromAnnotations("cookie-maxlife", service.Annotations, ingress.Annotations, c.cfg.ConfigMap.Annotations)
	cookieAnnotations["cookie-nocache"], _ = GetValueFromAnnotations("cookie-nocache", service.Annotations, ingress.Annotations, c.cfg.ConfigMap.Annotations)
	cookieAnnotations["cookie-postonly"], _ = GetValueFromAnnotations("cookie-postonly", service.Annotations, ingress.Annotations, c.cfg.ConfigMap.Annotations)
	cookieAnnotations["cookie-preserve"], _ = GetValueFromAnnotations("cookie-preserve", service.Annotations, ingress.Annotations, c.cfg.ConfigMap.Annotations)
	cookieAnnotations["cookie-secure"], _ = GetValueFromAnnotations("cookie-secure", service.Annotations, ingress.Annotations, c.cfg.ConfigMap.Annotations)
	cookieAnnotations["cookie-type"], _ = GetValueFromAnnotations("cookie-type", service.Annotations, ingress.Annotations, c.cfg.ConfigMap.Annotations)
	cookie := models.Cookie{}
	for k, v := range cookieAnnotations {
		if v == nil {
			continue
		}
		switch k {
		case "cookie-domain":
			var domains []*models.Domain
			for _, domain := range strings.Fields(v.Value) {
				domains = append(domains, &models.Domain{Value: domain})
			}
			cookie.Domains = domains
		case "cookie-dynamic":
			dynamic, err := utils.GetBoolValue(v.Value, "cookie-dynamic")
			c.Logger.Error(err)
			cookie.Dynamic = dynamic
		case "cookie-httponly":
			httponly, err := utils.GetBoolValue(v.Value, "cookie-httponly")
			c.Logger.Error(err)
			cookie.Httponly = httponly
		case "cookie-indirect":
			indirect, err := utils.GetBoolValue(v.Value, "cookie-indirect")
			c.Logger.Error(err)
			cookie.Indirect = indirect
		case "cookie-maxidle":
			maxidle, err := strconv.ParseInt(v.Value, 10, 64)
			c.Logger.Error(err)
			cookie.Maxidle = maxidle
		case "cookie-maxlife":
			maxlife, err := strconv.ParseInt(v.Value, 10, 64)
			c.Logger.Error(err)
			cookie.Maxlife = maxlife
		case "cookie-nocache":
			nocache, err := utils.GetBoolValue(v.Value, "cookie-nocache")
			c.Logger.Error(err)
			cookie.Nocache = nocache
		case "cookie-persistence":
			cookie.Name = utils.PtrString(v.Value)
		case "cookie-postonly":
			postonly, err := utils.GetBoolValue(v.Value, "cookie-postonly")
			c.Logger.Error(err)
			cookie.Postonly = postonly
		case "cookie-preserve":
			preserve, err := utils.GetBoolValue(v.Value, "cookie-preserve")
			c.Logger.Error(err)
			cookie.Preserve = preserve
		case "cookie-secure":
			secure, err := utils.GetBoolValue(v.Value, "cookie-secure")
			c.Logger.Error(err)
			cookie.Secure = secure
		case "cookie-type":
			cookie.Type = v.Value
		}
	}
	return cookie
}

func (c *HAProxyController) getServerAnnotations(ingress *Ingress, service *Service) (srvAnnotations map[string]*StringW, activeAnnotations bool) {
	srvAnnotations = make(map[string]*StringW, 5)
	srvAnnotations["cookie-persistence"], _ = GetValueFromAnnotations("cookie-persistence", service.Annotations, ingress.Annotations, c.cfg.ConfigMap.Annotations)
	srvAnnotations["check"], _ = GetValueFromAnnotations("check", service.Annotations, ingress.Annotations, c.cfg.ConfigMap.Annotations)
	srvAnnotations["check-interval"], _ = GetValueFromAnnotations("check-interval", service.Annotations, ingress.Annotations, c.cfg.ConfigMap.Annotations)
	srvAnnotations["pod-maxconn"], _ = GetValueFromAnnotations("pod-maxconn", service.Annotations)
	srvAnnotations["server-ssl"], _ = GetValueFromAnnotations("server-ssl", service.Annotations, ingress.Annotations, c.cfg.ConfigMap.Annotations)
	srvAnnotations["send-proxy-protocol"], _ = GetValueFromAnnotations("send-proxy-protocol", service.Annotations)
	for k, v := range srvAnnotations {
		if v == nil {
			delete(srvAnnotations, k)
			continue
		}
		if v.Status != EMPTY {
			activeAnnotations = true
			break
		}
	}
	return srvAnnotations, activeAnnotations
}
