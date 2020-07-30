/*
Copyright 2014 The Kubernetes Authors.

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

package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"k8s.io/client-go/tools/clientcmd"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/kubernetes/pkg/kubectl/cmd/annotate"
	"k8s.io/kubernetes/pkg/kubectl/cmd/apiresources"
	"k8s.io/kubernetes/pkg/kubectl/cmd/apply"
	"k8s.io/kubernetes/pkg/kubectl/cmd/attach"
	"k8s.io/kubernetes/pkg/kubectl/cmd/auth"
	"k8s.io/kubernetes/pkg/kubectl/cmd/autoscale"
	"k8s.io/kubernetes/pkg/kubectl/cmd/certificates"
	"k8s.io/kubernetes/pkg/kubectl/cmd/clusterinfo"
	"k8s.io/kubernetes/pkg/kubectl/cmd/completion"
	cmdconfig "k8s.io/kubernetes/pkg/kubectl/cmd/config"
	"k8s.io/kubernetes/pkg/kubectl/cmd/convert"
	"k8s.io/kubernetes/pkg/kubectl/cmd/cp"
	"k8s.io/kubernetes/pkg/kubectl/cmd/create"
	"k8s.io/kubernetes/pkg/kubectl/cmd/delete"
	"k8s.io/kubernetes/pkg/kubectl/cmd/describe"
	"k8s.io/kubernetes/pkg/kubectl/cmd/diff"
	"k8s.io/kubernetes/pkg/kubectl/cmd/drain"
	"k8s.io/kubernetes/pkg/kubectl/cmd/edit"
	cmdexec "k8s.io/kubernetes/pkg/kubectl/cmd/exec"
	"k8s.io/kubernetes/pkg/kubectl/cmd/explain"
	"k8s.io/kubernetes/pkg/kubectl/cmd/expose"
	"k8s.io/kubernetes/pkg/kubectl/cmd/get"
	"k8s.io/kubernetes/pkg/kubectl/cmd/label"
	"k8s.io/kubernetes/pkg/kubectl/cmd/logs"
	"k8s.io/kubernetes/pkg/kubectl/cmd/options"
	"k8s.io/kubernetes/pkg/kubectl/cmd/patch"
	"k8s.io/kubernetes/pkg/kubectl/cmd/plugin"
	"k8s.io/kubernetes/pkg/kubectl/cmd/portforward"
	"k8s.io/kubernetes/pkg/kubectl/cmd/proxy"
	"k8s.io/kubernetes/pkg/kubectl/cmd/replace"
	"k8s.io/kubernetes/pkg/kubectl/cmd/rollingupdate"
	"k8s.io/kubernetes/pkg/kubectl/cmd/rollout"
	"k8s.io/kubernetes/pkg/kubectl/cmd/run"
	"k8s.io/kubernetes/pkg/kubectl/cmd/scale"
	"k8s.io/kubernetes/pkg/kubectl/cmd/set"
	"k8s.io/kubernetes/pkg/kubectl/cmd/taint"
	"k8s.io/kubernetes/pkg/kubectl/cmd/top"
	cmdutil "k8s.io/kubernetes/pkg/kubectl/cmd/util"
	"k8s.io/kubernetes/pkg/kubectl/cmd/version"
	"k8s.io/kubernetes/pkg/kubectl/cmd/wait"
	"k8s.io/kubernetes/pkg/kubectl/util/i18n"
	"k8s.io/kubernetes/pkg/kubectl/util/templates"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/kubernetes/pkg/kubectl/cmd/kustomize"
)

const (
	bashCompletionFunc = `# call kubectl get $1,
__kubectl_override_flag_list=(--kubeconfig --cluster --user --context --namespace --server -n -s)
__kubectl_override_flags()
{
    local ${__kubectl_override_flag_list[*]##*-} two_word_of of var
    for w in "${words[@]}"; do
        if [ -n "${two_word_of}" ]; then
            eval "${two_word_of##*-}=\"${two_word_of}=\${w}\""
            two_word_of=
            continue
        fi
        for of in "${__kubectl_override_flag_list[@]}"; do
            case "${w}" in
                ${of}=*)
                    eval "${of##*-}=\"${w}\""
                    ;;
                ${of})
                    two_word_of="${of}"
                    ;;
            esac
        done
    done
    for var in "${__kubectl_override_flag_list[@]##*-}"; do
        if eval "test -n \"\$${var}\""; then
            eval "echo \${${var}}"
        fi
    done
}

__kubectl_config_get_contexts()
{
    __kubectl_parse_config "contexts"
}

__kubectl_config_get_clusters()
{
    __kubectl_parse_config "clusters"
}

__kubectl_config_get_users()
{
    __kubectl_parse_config "users"
}

# $1 has to be "contexts", "clusters" or "users"
__kubectl_parse_config()
{
    local template kubectl_out
    template="{{ range .$1  }}{{ .name }} {{ end }}"
    if kubectl_out=$(kubectl config $(__kubectl_override_flags) -o template --template="${template}" view 2>/dev/null); then
        COMPREPLY=( $( compgen -W "${kubectl_out[*]}" -- "$cur" ) )
    fi
}

# $1 is the name of resource (required)
# $2 is template string for kubectl get (optional)
__kubectl_parse_get()
{
    local template
    template="${2:-"{{ range .items  }}{{ .metadata.name }} {{ end }}"}"
    local kubectl_out
    if kubectl_out=$(kubectl get $(__kubectl_override_flags) -o template --template="${template}" "$1" 2>/dev/null); then
        COMPREPLY+=( $( compgen -W "${kubectl_out[*]}" -- "$cur" ) )
    fi
}

__kubectl_get_resource()
{
    if [[ ${#nouns[@]} -eq 0 ]]; then
      local kubectl_out
      if kubectl_out=$(kubectl api-resources $(__kubectl_override_flags) -o name --cached --request-timeout=5s --verbs=get 2>/dev/null); then
          COMPREPLY=( $( compgen -W "${kubectl_out[*]}" -- "$cur" ) )
          return 0
      fi
      return 1
    fi
    __kubectl_parse_get "${nouns[${#nouns[@]} -1]}"
}

__kubectl_get_resource_namespace()
{
    __kubectl_parse_get "namespace"
}

__kubectl_get_resource_pod()
{
    __kubectl_parse_get "pod"
}

__kubectl_get_resource_rc()
{
    __kubectl_parse_get "rc"
}

__kubectl_get_resource_node()
{
    __kubectl_parse_get "node"
}

__kubectl_get_resource_clusterrole()
{
    __kubectl_parse_get "clusterrole"
}

# $1 is the name of the pod we want to get the list of containers inside
__kubectl_get_containers()
{
    local template
    template="{{ range .spec.initContainers }}{{ .name }} {{end}}{{ range .spec.containers  }}{{ .name }} {{ end }}"
    __kubectl_debug "${FUNCNAME} nouns are ${nouns[*]}"

    local len="${#nouns[@]}"
    if [[ ${len} -ne 1 ]]; then
        return
    fi
    local last=${nouns[${len} -1]}
    local kubectl_out
    if kubectl_out=$(kubectl get $(__kubectl_override_flags) -o template --template="${template}" pods "${last}" 2>/dev/null); then
        COMPREPLY=( $( compgen -W "${kubectl_out[*]}" -- "$cur" ) )
    fi
}

# Require both a pod and a container to be specified
__kubectl_require_pod_and_container()
{
    if [[ ${#nouns[@]} -eq 0 ]]; then
        __kubectl_parse_get pods
        return 0
    fi;
    __kubectl_get_containers
    return 0
}

__kubectl_cp()
{
    if [[ $(type -t compopt) = "builtin" ]]; then
        compopt -o nospace
    fi

    case "$cur" in
        /*|[.~]*) # looks like a path
            return
            ;;
        *:*) # TODO: complete remote files in the pod
            return
            ;;
        */*) # complete <namespace>/<pod>
            local template namespace kubectl_out
            template="{{ range .items }}{{ .metadata.namespace }}/{{ .metadata.name }}: {{ end }}"
            namespace="${cur%%/*}"
            if kubectl_out=( $(kubectl get $(__kubectl_override_flags) --namespace "${namespace}" -o template --template="${template}" pods 2>/dev/null) ); then
                COMPREPLY=( $(compgen -W "${kubectl_out[*]}" -- "${cur}") )
            fi
            return
            ;;
        *) # complete namespaces, pods, and filedirs
            __kubectl_parse_get "namespace" "{{ range .items  }}{{ .metadata.name }}/ {{ end }}"
            __kubectl_parse_get "pod" "{{ range .items  }}{{ .metadata.name }}: {{ end }}"
            _filedir
            ;;
    esac
}

__custom_func() {
    case ${last_command} in
        kubectl_get | kubectl_describe | kubectl_delete | kubectl_label | kubectl_edit | kubectl_patch |\
        kubectl_annotate | kubectl_expose | kubectl_scale | kubectl_autoscale | kubectl_taint | kubectl_rollout_* |\
        kubectl_apply_edit-last-applied | kubectl_apply_view-last-applied)
            __kubectl_get_resource
            return
            ;;
        kubectl_logs)
            __kubectl_require_pod_and_container
            return
            ;;
        kubectl_exec | kubectl_port-forward | kubectl_top_pod | kubectl_attach)
            __kubectl_get_resource_pod
            return
            ;;
        kubectl_rolling-update)
            __kubectl_get_resource_rc
            return
            ;;
        kubectl_cordon | kubectl_uncordon | kubectl_drain | kubectl_top_node)
            __kubectl_get_resource_node
            return
            ;;
        kubectl_config_use-context | kubectl_config_rename-context)
            __kubectl_config_get_contexts
            return
            ;;
        kubectl_config_delete-cluster)
            __kubectl_config_get_clusters
            return
            ;;
        kubectl_cp)
            __kubectl_cp
            return
            ;;
        *)
            ;;
    esac
}
`
)

var (
	bashCompletionFlags = map[string]string{
		"namespace": "__kubectl_get_resource_namespace",
		"context":   "__kubectl_config_get_contexts",
		"cluster":   "__kubectl_config_get_clusters",
		"user":      "__kubectl_config_get_users",
	}
)

// NewDefaultKubectlCommand creates the `kubectl` command with default arguments
func NewDefaultKubectlCommand() *cobra.Command {
	return NewDefaultKubectlCommandWithArgs(NewDefaultPluginHandler(plugin.ValidPluginFilenamePrefixes), os.Args, os.Stdin, os.Stdout, os.Stderr)
}

// NewDefaultKubectlCommandWithArgs creates the `kubectl` command with arguments
func NewDefaultKubectlCommandWithArgs(pluginHandler PluginHandler, args []string, in io.Reader, out, errout io.Writer) *cobra.Command {
	cmd := NewKubectlCommand(in, out, errout)

	if pluginHandler == nil {
		return cmd
	}

	if len(args) > 1 {
		cmdPathPieces := args[1:]

		// only look for suitable extension executables if
		// the specified command does not already exist
		if _, _, err := cmd.Find(cmdPathPieces); err != nil {
			if err := HandlePluginCommand(pluginHandler, cmdPathPieces); err != nil {
				fmt.Fprintf(errout, "%v\n", err)
				os.Exit(1)
			}
		}
	}

	return cmd
}

// PluginHandler is capable of parsing command line arguments
// and performing executable filename lookups to search
// for valid plugin files, and execute found plugins.
type PluginHandler interface {
	// exists at the given filename, or a boolean false.
	// Lookup will iterate over a list of given prefixes
	// in order to recognize valid plugin filenames.
	// The first filepath to match a prefix is returned.
	Lookup(filename string) (string, bool)
	// Execute receives an executable's filepath, a slice
	// of arguments, and a slice of environment variables
	// to relay to the executable.
	Execute(executablePath string, cmdArgs, environment []string) error
}

// DefaultPluginHandler implements PluginHandler
type DefaultPluginHandler struct {
	ValidPrefixes []string
}

// NewDefaultPluginHandler instantiates the DefaultPluginHandler with a list of
// given filename prefixes used to identify valid plugin filenames.
func NewDefaultPluginHandler(validPrefixes []string) *DefaultPluginHandler {
	return &DefaultPluginHandler{
		ValidPrefixes: validPrefixes,
	}
}

// Lookup implements PluginHandler
func (h *DefaultPluginHandler) Lookup(filename string) (string, bool) {
	for _, prefix := range h.ValidPrefixes {
		path, err := exec.LookPath(fmt.Sprintf("%s-%s", prefix, filename))
		if err != nil || len(path) == 0 {
			continue
		}
		return path, true
	}

	return "", false
}

// Execute implements PluginHandler
func (h *DefaultPluginHandler) Execute(executablePath string, cmdArgs, environment []string) error {
	return syscall.Exec(executablePath, cmdArgs, environment)
}

// HandlePluginCommand receives a pluginHandler and command-line arguments and attempts to find
// a plugin executable on the PATH that satisfies the given arguments.
func HandlePluginCommand(pluginHandler PluginHandler, cmdArgs []string) error {
	remainingArgs := []string{} // all "non-flag" arguments

	for idx := range cmdArgs {
		if strings.HasPrefix(cmdArgs[idx], "-") {
			break
		}
		remainingArgs = append(remainingArgs, strings.Replace(cmdArgs[idx], "-", "_", -1))
	}

	foundBinaryPath := ""

	// attempt to find binary, starting at longest possible name with given cmdArgs
	for len(remainingArgs) > 0 {
		path, found := pluginHandler.Lookup(strings.Join(remainingArgs, "-"))
		if !found {
			remainingArgs = remainingArgs[:len(remainingArgs)-1]
			continue
		}

		foundBinaryPath = path
		break
	}

	if len(foundBinaryPath) == 0 {
		return nil
	}

	// invoke cmd binary relaying the current environment and args given
	// remainingArgs will always have at least one element.
	// execve will make remainingArgs[0] the "binary name".
	if err := pluginHandler.Execute(foundBinaryPath, append([]string{foundBinaryPath}, cmdArgs[len(remainingArgs):]...), os.Environ()); err != nil {
		return err
	}

	return nil
}

// NewKubectlCommand creates the `kubectl` command and its nested children.
func NewKubectlCommand(in io.Reader, out, err io.Writer) *cobra.Command {
	// Parent command to which all subcommands are added.
	cmds := &cobra.Command{
		Use:   "kubectl",
		Short: i18n.T("kubectl controls the Kubernetes cluster manager"),
		Long: templates.LongDesc(`
      kubectl controls the Kubernetes cluster manager.

      Find more information at:
            https://kubernetes.io/docs/reference/kubectl/overview/`),
		Run: runHelp,
		// Hook before and after Run initialize and write profiles to disk,
		// respectively.
		PersistentPreRunE: func(*cobra.Command, []string) error {
			return initProfiling()
		},
		PersistentPostRunE: func(*cobra.Command, []string) error {
			return flushProfiling()
		},
		BashCompletionFunction: bashCompletionFunc,
	}

	flags := cmds.PersistentFlags()
	flags.SetNormalizeFunc(cliflag.WarnWordSepNormalizeFunc) // Warn for "_" flags

	// Normalize all flags that are coming from other packages or pre-configurations
	// a.k.a. change all "_" to "-". e.g. glog package
	flags.SetNormalizeFunc(cliflag.WordSepNormalizeFunc)

	addProfilingFlags(flags)

	kubeConfigFlags := genericclioptions.NewConfigFlags(true).WithDeprecatedPasswordFlag()
	kubeConfigFlags.AddFlags(flags)
	matchVersionKubeConfigFlags := cmdutil.NewMatchVersionFlags(kubeConfigFlags)
	matchVersionKubeConfigFlags.AddFlags(cmds.PersistentFlags())

	cmds.PersistentFlags().AddGoFlagSet(flag.CommandLine)

	// 参数初始化
	f := cmdutil.NewFactory(matchVersionKubeConfigFlags)

	// Sending in 'nil' for the getLanguageFn() results in using
	// the LANG environment variable.
	//
	// TODO: Consider adding a flag or file preference for setting
	// the language, instead of just loading from the LANG env. variable.
	i18n.LoadTranslations("kubectl", nil)

	// From this point and forward we get warnings on flags that contain "_" separators
	cmds.SetGlobalNormalizationFunc(cliflag.WarnWordSepNormalizeFunc)

	ioStreams := genericclioptions.IOStreams{In: in, Out: out, ErrOut: err}

	// 命令初始化
	groups := templates.CommandGroups{
		{
			// 基础命令（初级)
			Message: "Basic Commands (Beginner):",
			Commands: []*cobra.Command{
				// create 通过json/yaml文件或标准输入创建一个资源对象，支持很多子命令，例如namespace，pod，service等
				create.NewCmdCreate(f, ioStreams),
				// expose 将json/yaml文件中定义的资源对象的端口暴露给新的service资源对象
				expose.NewCmdExposeService(f, ioStreams),
				// run 创建并运行一个或多个容器镜像
				run.NewCmdRun(f, ioStreams),
				// set 配置资源对象，设置特定功能
				set.NewCmdSet(f, ioStreams),
			},
		},
		{
			// 基础命令（中级）
			Message: "Basic Commands (Intermediate):",
			Commands: []*cobra.Command{
				// explain 查看资源对象详细信息
				explain.NewCmdExplain("kubectl", f, ioStreams),
				// get 获取一个或多个资源对象的信息
				get.NewCmdGet("kubectl", f, ioStreams),
				// edit 使用默认编辑器编辑定义的资源对象
				edit.NewCmdEdit(f, ioStreams),
				// delete 通过json/yaml文件、标准输入、资源名称或者标签选择器来删除资源
				delete.NewCmdDelete(f, ioStreams),
			},
		},
		{
			// 部署命令
			Message: "Deploy Commands:",
			Commands: []*cobra.Command{
				// rollout 管理资源对象的部署
				rollout.NewCmdRollout(f, ioStreams),
				// rolling-update 使用RC（ReplicationController)进行滚动更新
				rollingupdate.NewCmdRollingUpdate(f, ioStreams),
				// scale 扩容或缩容Deployment、ReplicateSet、ReplicationController等
				scale.NewCmdScale(f, ioStreams),
				// autoscale 自动设置在kubernetes系统中运行的Pod数量（水平自动伸缩）
				autoscale.NewCmdAutoscale(f, ioStreams),
			},
		},
		{
			// 集群管理命令
			Message: "Cluster Management Commands:",
			Commands: []*cobra.Command{
				// certificate 修改证书资源对象
				certificates.NewCmdCertificate(f, ioStreams),
				// cluster-info 查看集群信息
				clusterinfo.NewCmdClusterInfo(f, ioStreams),
				// top 显示资源（CPU、内存、存储）使用情况
				top.NewCmdTop(f, ioStreams),
				// cordon 将指定节点标记为不可调度
				drain.NewCmdCordon(f, ioStreams),
				// uncordon 将指定节点标记为可调度
				drain.NewCmdUncordon(f, ioStreams),
				// drain 安全的驱逐指定节点的所有Pod
				drain.NewCmdDrain(f, ioStreams),
				// taint 将一个或多个节点设置为污点
				taint.NewCmdTaint(f, ioStreams),
			},
		},
		{
			// 故障排查，调试命令
			Message: "Troubleshooting and Debugging Commands:",
			Commands: []*cobra.Command{
				// describe 显示一个或多个资源对象的详细信息
				describe.NewCmdDescribe("kubectl", f, ioStreams),
				// 输出Pod资源对象中一个容器的日志
				logs.NewCmdLogs(f, ioStreams),
				// attach 连接到一个正在运行的容器
				attach.NewCmdAttach(f, ioStreams),
				// exec 在指定的容器内执行命令
				cmdexec.NewCmdExec(f, ioStreams),
				// port-forward 将本机指定端口映射到Pod资源对象的端口
				portforward.NewCmdPortForward(f, ioStreams),
				// proxy 运行一个 proxy 到 Kubernetes API server
				proxy.NewCmdProxy(f, ioStreams),
				// cp 用于Pod和主机交换文件
				cp.NewCmdCp(f, ioStreams),
				// auth 检查验证
				auth.NewCmdAuth(f, ioStreams),
			},
		},
		{
			// 高级命令
			Message: "Advanced Commands:",
			Commands: []*cobra.Command{
				// diff 对比本地Json/Yaml文件与kube-apiserver中运行的配置文件是否有差异
				diff.NewCmdDiff(f, ioStreams),
				// apply 通过JSON/Yaml 文件、标准输入对资源对象进行配置更新
				apply.NewCmdApply("kubectl", f, ioStreams),
				// patch 通过patch方式修改资源对象字段
				patch.NewCmdPatch(f, ioStreams),
				// replace 通过Json/Yaml 文件或标准输入来替换资源对象
				replace.NewCmdReplace(f, ioStreams),
				// wait 在一个或多个资源上等待条件达成
				wait.NewCmdWait(f, ioStreams),
				// convert 转换Json/Yaml 文件为不同的资源版本
				convert.NewCmdConvert(f, ioStreams),
				// kustomize 定制kubernetes配置
				kustomize.NewCmdKustomize(ioStreams),
			},
		},
		{
			// 设置命令
			Message: "Settings Commands:",
			Commands: []*cobra.Command{
				// label 增删改资源标签
				label.NewCmdLabel(f, ioStreams),
				// annotate 更新一个或多个资源对象的注释信息
				annotate.NewCmdAnnotate("kubectl", f, ioStreams),
				// 命令行自动补全
				completion.NewCmdCompletion(ioStreams.Out, ""),
			},
		},
	}
	// 添加到子命令
	groups.Add(cmds)

	filters := []string{"options"}

	// Hide the "alpha" subcommand if there are no alpha commands in this build.
	alpha := NewCmdAlpha(f, ioStreams)
	if !alpha.HasSubCommands() {
		filters = append(filters, alpha.Name())
	}

	templates.ActsAsRootCommand(cmds, filters, groups...)

	for name, completion := range bashCompletionFlags {
		if cmds.Flag(name) != nil {
			if cmds.Flag(name).Annotations == nil {
				cmds.Flag(name).Annotations = map[string][]string{}
			}
			cmds.Flag(name).Annotations[cobra.BashCompCustom] = append(
				cmds.Flag(name).Annotations[cobra.BashCompCustom],
				completion,
			)
		}
	}

	cmds.AddCommand(alpha)
	// config 管理kubeconfig配置文件
	cmds.AddCommand(cmdconfig.NewCmdConfig(f, clientcmd.NewDefaultPathOptions(), ioStreams))
	// plugin 运行命令插件功能
	cmds.AddCommand(plugin.NewCmdPlugin(f, ioStreams))
	// version 查看客户端和服务端的系统版本信息
	cmds.AddCommand(version.NewCmdVersion(f, ioStreams))
	// api-versions 列出当前kubernetes系统支持的资源组和资源版本，表现形式为 group/version
	cmds.AddCommand(apiresources.NewCmdAPIVersions(f, ioStreams))
	// api-resources 列出当前系统支持的Resource资源列表
	cmds.AddCommand(apiresources.NewCmdAPIResources(f, ioStreams))
	// options 查看支持的参数列表
	cmds.AddCommand(options.NewCmdOptions(ioStreams.Out))

	return cmds
}

func runHelp(cmd *cobra.Command, args []string) {
	cmd.Help()
}

// deprecatedAlias is intended to be used to create a "wrapper" command around
// an existing command. The wrapper works the same but prints a deprecation
// message before running. This command is identical functionality.
func deprecatedAlias(deprecatedVersion string, cmd *cobra.Command) *cobra.Command {
	// Have to be careful here because Cobra automatically extracts the name
	// of the command from the .Use field.
	originalName := cmd.Name()

	cmd.Use = deprecatedVersion
	cmd.Deprecated = fmt.Sprintf("use %q instead", originalName)
	cmd.Short = fmt.Sprintf("%s. This command is deprecated, use %q instead", cmd.Short, originalName)
	cmd.Hidden = true
	return cmd
}
