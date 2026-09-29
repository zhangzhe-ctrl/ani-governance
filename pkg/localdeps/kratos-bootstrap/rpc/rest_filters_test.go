package rpc

import (
 "net/http"
 "net/http/httptest"
 "testing"
 khttp "github.com/go-kratos/kratos/v2/transport/http"
 conf "go-wind-admin/pkg/localdeps/kratos-bootstrap/api/gen/go/conf/v1"
)
func TestAdditionalHTTPFilterPreservesCORS(t *testing.T){
 called:=false
 extra:=func(next http.Handler)http.Handler{return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){called=true;w.Header().Set("X-Boundary","checked");next.ServeHTTP(w,r)})}
 cfg:=&conf.Bootstrap{Server:&conf.Server{Rest:&conf.Server_REST{Cors:&conf.Server_REST_CORS{Origins:[]string{"https://console.example.test"},Methods:[]string{"GET"}}}}}
 srv,err:=CreateRestServerWithFilters(cfg,[]khttp.FilterFunc{extra});if err!=nil{t.Fatal(err)}
 srv.HandleFunc("/test",func(w http.ResponseWriter,_ *http.Request){w.WriteHeader(http.StatusNoContent)})
 r:=httptest.NewRequest("GET","/test",nil);r.Header.Set("Origin","https://console.example.test");w:=httptest.NewRecorder();srv.ServeHTTP(w,r)
 if w.Code!=204||!called||w.Header().Get("X-Boundary")!="checked"||w.Header().Get("Access-Control-Allow-Origin")!="https://console.example.test"{t.Fatal("additional filter displaced CORS",w.Code,w.Header())}
}
