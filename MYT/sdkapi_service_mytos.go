package main

// sdkapi_service_mytos.go 原始内容已按职责拆分到：
//   - sdkapi_service_mytos_stream.go   （推流/S5代理/GPS/摇一摇/Google证书 + getMytosClientForAndroid）
//   - sdkapi_service_mytos_v3.go       （容器/模拟器信息查询与配置 V3 接口）
//   - sdkapi_service_mytos_container.go（容器机型切换/异步随机设备信息）
//
// 拆分原则：仅移动代码块，保持 package main 与接收者不变，不改任何逻辑。
