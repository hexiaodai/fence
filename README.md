# Fence

Fence 是一个开源项目，用于自动管理自定义资源 `Sidecar`。

## 背景

服务网格内服务数量过多时，Envoy 配置量太大，新上的应用长时间处于 Not Ready 状态。为此运维人员需要管理自定义资源 `Sidecar`，手动为应用配置服务依赖关系。

Fence 拥有自动获取服务依赖关系的能力，提供自动管理自定义资源 `Sidecar`。

## 架构

![架构图](docs/images/fence.png)

## 安装

```shell
kubectl create namespace fence

kubectl apply -f "https://raw.githubusercontent.com/hexiaodai/fence/main/deploy/fence.yaml"
```
