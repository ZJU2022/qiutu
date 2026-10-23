# 康复自重训练方案

给康复师看的一页方案：按康复师写的进退阶排列，俯卧撑阶梯保留。打开网址就能看到全部动作、升级规则和肌肉图。

## 在自己电脑上打开

需要已安装 Go。

```bash
go run ./cmd/server
```

浏览器打开 http://localhost:8080

## 在 CentOS 7 云主机上部署

```bash
git clone https://github.com/ZJU2022/qiutu.git
cd qiutu
sudo bash deploy.sh
```

脚本会安装编译用的 Go、编译程序、设置开机自启，并在本机防火墙放行 8080。

云主机的安全组也要放行 TCP 8080。完成后用 `http://公网IP:8080` 打开。

以后更新代码：

```bash
git pull
sudo bash deploy.sh
```
