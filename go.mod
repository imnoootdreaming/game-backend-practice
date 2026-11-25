// 模块声明：Go 用这个名字作为本项目的唯一标识，其他包引用它时也要用这个名字
module game-backend-practice

// 项目要求的最低 Go 版本（本机 Go 1.27.4 满足）
go 1.27

// 依赖清单：本项目的第三方库。yaml.v3 负责把 configs/config.yaml 解析成 Go 结构体
require gopkg.in/yaml.v3 v3.0.1
