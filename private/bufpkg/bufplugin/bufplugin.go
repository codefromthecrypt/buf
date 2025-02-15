// Copyright 2020-2023 Buf Technologies, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bufplugin

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bufbuild/buf/private/bufpkg/bufplugin/bufpluginconfig"
	"github.com/bufbuild/buf/private/bufpkg/bufplugin/bufpluginref"
	registryv1alpha1 "github.com/bufbuild/buf/private/gen/proto/go/buf/alpha/registry/v1alpha1"
)

// Plugin represents a plugin defined by a buf.plugin.yaml.
type Plugin interface {
	// Version is the version of the plugin's implementation
	// (e.g the protoc-gen-connect-go implementation is v0.2.0).
	Version() string
	// SourceURL is an optional attribute used to specify where the source
	// for the plugin can be found.
	SourceURL() string
	// Description is an optional attribute to provide a more detailed
	// description for the plugin.
	Description() string
	// Dependencies are the dependencies this plugin has on other plugins.
	//
	// An example of a dependency might be a 'protoc-gen-go-grpc' plugin
	// which depends on the 'protoc-gen-go' generated code.
	Dependencies() []bufpluginref.PluginReference
	// Registry is the registry configuration, which lets the user specify
	// registry dependencies, and other metadata that applies to a specific
	// remote generation registry (e.g. the Go module proxy, NPM registry,
	// etc).
	Registry() *bufpluginconfig.RegistryConfig
	// ContainerImageDigest returns the plugin's source image digest.
	//
	// For now we only support docker image sources, but this
	// might evolve to support others later on.
	ContainerImageDigest() string
}

// NewPlugin creates a new plugin from the given configuration and image digest.
func NewPlugin(
	version string,
	dependencies []bufpluginref.PluginReference,
	registryConfig *bufpluginconfig.RegistryConfig,
	imageDigest string,
	sourceURL string,
	description string,
) (Plugin, error) {
	return newPlugin(version, dependencies, registryConfig, imageDigest, sourceURL, description)
}

// PluginToProtoPluginRegistryType determines the appropriate registryv1alpha1.PluginRegistryType for the plugin.
func PluginToProtoPluginRegistryType(plugin Plugin) registryv1alpha1.PluginRegistryType {
	registryType := registryv1alpha1.PluginRegistryType_PLUGIN_REGISTRY_TYPE_UNSPECIFIED
	if plugin.Registry() != nil {
		if plugin.Registry().Go != nil {
			registryType = registryv1alpha1.PluginRegistryType_PLUGIN_REGISTRY_TYPE_GO
		} else if plugin.Registry().NPM != nil {
			registryType = registryv1alpha1.PluginRegistryType_PLUGIN_REGISTRY_TYPE_NPM
		} else if plugin.Registry().Maven != nil {
			registryType = registryv1alpha1.PluginRegistryType_PLUGIN_REGISTRY_TYPE_MAVEN
		}
	}
	return registryType
}

// OutputLanguagesToProtoLanguages determines the appropriate registryv1alpha1.PluginRegistryType for the plugin.
func OutputLanguagesToProtoLanguages(languages []string) ([]registryv1alpha1.PluginLanguage, error) {
	languageToEnum := make(map[string]registryv1alpha1.PluginLanguage)
	var supportedLanguages []string
	for pluginLanguageKey, pluginLanguage := range registryv1alpha1.PluginLanguage_value {
		if pluginLanguage == 0 {
			continue
		}
		pluginLanguageKey := strings.TrimPrefix(pluginLanguageKey, "PLUGIN_LANGUAGE_")
		pluginLanguageKey = strings.ToLower(pluginLanguageKey)
		// Example:
		// { go: 1, javascript: 2 }
		languageToEnum[pluginLanguageKey] = registryv1alpha1.PluginLanguage(pluginLanguage)
		supportedLanguages = append(supportedLanguages, pluginLanguageKey)
	}
	sort.Strings(supportedLanguages)
	var protoLanguages []registryv1alpha1.PluginLanguage
	for _, language := range languages {
		if pluginLanguage, ok := languageToEnum[language]; ok {
			protoLanguages = append(protoLanguages, pluginLanguage)
			continue
		}
		return nil, fmt.Errorf("invalid plugin output language: %q\nsupported languages: %s", language, strings.Join(supportedLanguages, ", "))
	}
	sort.Slice(protoLanguages, func(i, j int) bool {
		return protoLanguages[i] < protoLanguages[j]
	})
	return protoLanguages, nil
}

// PluginRegistryToProtoRegistryConfig converts a bufpluginconfig.RegistryConfig to a registryv1alpha1.RegistryConfig.
func PluginRegistryToProtoRegistryConfig(pluginRegistry *bufpluginconfig.RegistryConfig) (*registryv1alpha1.RegistryConfig, error) {
	if pluginRegistry == nil {
		return nil, nil
	}
	registryConfig := &registryv1alpha1.RegistryConfig{
		Options: bufpluginconfig.PluginOptionsToOptionsSlice(pluginRegistry.Options),
	}
	if pluginRegistry.Go != nil {
		goConfig := &registryv1alpha1.GoConfig{}
		goConfig.MinimumVersion = pluginRegistry.Go.MinVersion
		if pluginRegistry.Go.Deps != nil {
			goConfig.RuntimeLibraries = make([]*registryv1alpha1.GoConfig_RuntimeLibrary, 0, len(pluginRegistry.Go.Deps))
			for _, dependency := range pluginRegistry.Go.Deps {
				goConfig.RuntimeLibraries = append(goConfig.RuntimeLibraries, goRuntimeDependencyToProtoGoRuntimeLibrary(dependency))
			}
		}
		registryConfig.RegistryConfig = &registryv1alpha1.RegistryConfig_GoConfig{GoConfig: goConfig}
	} else if pluginRegistry.NPM != nil {
		importStyle, err := npmImportStyleToNPMProtoImportStyle(pluginRegistry.NPM.ImportStyle)
		if err != nil {
			return nil, err
		}
		npmConfig := &registryv1alpha1.NPMConfig{
			RewriteImportPathSuffix: pluginRegistry.NPM.RewriteImportPathSuffix,
			ImportStyle:             importStyle,
		}
		if pluginRegistry.NPM.Deps != nil {
			npmConfig.RuntimeLibraries = make([]*registryv1alpha1.NPMConfig_RuntimeLibrary, 0, len(pluginRegistry.NPM.Deps))
			for _, dependency := range pluginRegistry.NPM.Deps {
				npmConfig.RuntimeLibraries = append(npmConfig.RuntimeLibraries, npmRuntimeDependencyToProtoNPMRuntimeLibrary(dependency))
			}
		}
		registryConfig.RegistryConfig = &registryv1alpha1.RegistryConfig_NpmConfig{NpmConfig: npmConfig}
	} else if pluginRegistry.Maven != nil {
		mavenConfig := &registryv1alpha1.MavenConfig{}
		var javaCompilerConfig *registryv1alpha1.MavenConfig_CompilerJavaConfig
		if compiler := pluginRegistry.Maven.Compiler.Java; compiler != (bufpluginconfig.MavenCompilerJavaConfig{}) {
			javaCompilerConfig = &registryv1alpha1.MavenConfig_CompilerJavaConfig{
				Encoding: compiler.Encoding,
				Release:  int32(compiler.Release),
				Source:   int32(compiler.Source),
				Target:   int32(compiler.Target),
			}
		}
		var kotlinCompilerConfig *registryv1alpha1.MavenConfig_CompilerKotlinConfig
		if compiler := pluginRegistry.Maven.Compiler.Kotlin; compiler != (bufpluginconfig.MavenCompilerKotlinConfig{}) {
			kotlinCompilerConfig = &registryv1alpha1.MavenConfig_CompilerKotlinConfig{
				Version:         compiler.Version,
				ApiVersion:      compiler.APIVersion,
				JvmTarget:       compiler.JVMTarget,
				LanguageVersion: compiler.LanguageVersion,
			}
		}
		if javaCompilerConfig != nil || kotlinCompilerConfig != nil {
			mavenConfig.Compiler = &registryv1alpha1.MavenConfig_CompilerConfig{
				Java:   javaCompilerConfig,
				Kotlin: kotlinCompilerConfig,
			}
		}
		if pluginRegistry.Maven.Deps != nil {
			mavenConfig.RuntimeLibraries = make([]*registryv1alpha1.MavenConfig_RuntimeLibrary, len(pluginRegistry.Maven.Deps))
			for i, dependency := range pluginRegistry.Maven.Deps {
				mavenConfig.RuntimeLibraries[i] = MavenDependencyConfigToProtoRuntimeLibrary(dependency)
			}
		}
		if pluginRegistry.Maven.AdditionalRuntimes != nil {
			mavenConfig.AdditionalRuntimes = make([]*registryv1alpha1.MavenConfig_RuntimeConfig, len(pluginRegistry.Maven.AdditionalRuntimes))
			for i, runtime := range pluginRegistry.Maven.AdditionalRuntimes {
				mavenConfig.AdditionalRuntimes[i] = MavenRuntimeConfigToProtoRuntimeConfig(runtime)
			}
		}
		registryConfig.RegistryConfig = &registryv1alpha1.RegistryConfig_MavenConfig{MavenConfig: mavenConfig}
	}
	return registryConfig, nil
}

// MavenDependencyConfigToProtoRuntimeLibrary converts a bufpluginconfig.MavenDependencyConfig to an equivalent registryv1alpha1.MavenConfig_RuntimeLibrary.
func MavenDependencyConfigToProtoRuntimeLibrary(dependency bufpluginconfig.MavenDependencyConfig) *registryv1alpha1.MavenConfig_RuntimeLibrary {
	return &registryv1alpha1.MavenConfig_RuntimeLibrary{
		GroupId:    dependency.GroupID,
		ArtifactId: dependency.ArtifactID,
		Version:    dependency.Version,
		Classifier: dependency.Classifier,
		Extension:  dependency.Extension,
	}
}

// ProtoRegistryConfigToPluginRegistry converts a registryv1alpha1.RegistryConfig to a bufpluginconfig.RegistryConfig .
func ProtoRegistryConfigToPluginRegistry(config *registryv1alpha1.RegistryConfig) (*bufpluginconfig.RegistryConfig, error) {
	if config == nil {
		return nil, nil
	}
	registryConfig := &bufpluginconfig.RegistryConfig{
		Options: bufpluginconfig.OptionsSliceToPluginOptions(config.Options),
	}
	if config.GetGoConfig() != nil {
		goConfig := &bufpluginconfig.GoRegistryConfig{}
		goConfig.MinVersion = config.GetGoConfig().GetMinimumVersion()
		runtimeLibraries := config.GetGoConfig().GetRuntimeLibraries()
		if runtimeLibraries != nil {
			goConfig.Deps = make([]*bufpluginconfig.GoRegistryDependencyConfig, 0, len(runtimeLibraries))
			for _, library := range runtimeLibraries {
				goConfig.Deps = append(goConfig.Deps, protoGoRuntimeLibraryToGoRuntimeDependency(library))
			}
		}
		registryConfig.Go = goConfig
	} else if config.GetNpmConfig() != nil {
		importStyle, err := npmProtoImportStyleToNPMImportStyle(config.GetNpmConfig().GetImportStyle())
		if err != nil {
			return nil, err
		}
		npmConfig := &bufpluginconfig.NPMRegistryConfig{
			RewriteImportPathSuffix: config.GetNpmConfig().GetRewriteImportPathSuffix(),
			ImportStyle:             importStyle,
		}
		runtimeLibraries := config.GetNpmConfig().GetRuntimeLibraries()
		if runtimeLibraries != nil {
			npmConfig.Deps = make([]*bufpluginconfig.NPMRegistryDependencyConfig, 0, len(runtimeLibraries))
			for _, library := range runtimeLibraries {
				npmConfig.Deps = append(npmConfig.Deps, protoNPMRuntimeLibraryToNPMRuntimeDependency(library))
			}
		}
		registryConfig.NPM = npmConfig
	} else if protoMavenConfig := config.GetMavenConfig(); protoMavenConfig != nil {
		mavenConfig, err := ProtoMavenConfigToMavenRegistryConfig(protoMavenConfig)
		if err != nil {
			return nil, err
		}
		registryConfig.Maven = mavenConfig
	}
	return registryConfig, nil
}

// ProtoMavenConfigToMavenRegistryConfig converts a registryv1alpha1.MavenConfig to a bufpluginconfig.MavenRegistryConfig.
func ProtoMavenConfigToMavenRegistryConfig(protoMavenConfig *registryv1alpha1.MavenConfig) (*bufpluginconfig.MavenRegistryConfig, error) {
	mavenConfig := &bufpluginconfig.MavenRegistryConfig{}
	if protoCompiler := protoMavenConfig.GetCompiler(); protoCompiler != nil {
		mavenConfig.Compiler = bufpluginconfig.MavenCompilerConfig{}
		if protoJavaCompiler := protoCompiler.GetJava(); protoJavaCompiler != nil {
			mavenConfig.Compiler.Java = bufpluginconfig.MavenCompilerJavaConfig{
				Encoding: protoJavaCompiler.GetEncoding(),
				Release:  int(protoJavaCompiler.GetRelease()),
				Source:   int(protoJavaCompiler.GetSource()),
				Target:   int(protoJavaCompiler.GetTarget()),
			}
		}
		if protoKotlinCompiler := protoCompiler.GetKotlin(); protoKotlinCompiler != nil {
			mavenConfig.Compiler.Kotlin = bufpluginconfig.MavenCompilerKotlinConfig{
				APIVersion:      protoKotlinCompiler.GetApiVersion(),
				JVMTarget:       protoKotlinCompiler.GetJvmTarget(),
				LanguageVersion: protoKotlinCompiler.GetLanguageVersion(),
				Version:         protoKotlinCompiler.GetVersion(),
			}
		}
	}
	runtimeLibraries := protoMavenConfig.GetRuntimeLibraries()
	if runtimeLibraries != nil {
		mavenConfig.Deps = make([]bufpluginconfig.MavenDependencyConfig, len(runtimeLibraries))
		for i, library := range runtimeLibraries {
			mavenConfig.Deps[i] = ProtoMavenRuntimeLibraryToDependencyConfig(library)
		}
	}
	additionalRuntimes := protoMavenConfig.GetAdditionalRuntimes()
	if additionalRuntimes != nil {
		mavenConfig.AdditionalRuntimes = make([]bufpluginconfig.MavenRuntimeConfig, len(additionalRuntimes))
		for i, additionalRuntime := range additionalRuntimes {
			runtime, err := MavenProtoRuntimeConfigToRuntimeConfig(additionalRuntime)
			if err != nil {
				return nil, err
			}
			mavenConfig.AdditionalRuntimes[i] = runtime
		}
	}
	return mavenConfig, nil
}

// MavenProtoRuntimeConfigToRuntimeConfig converts a registryv1alpha1.MavenConfig_RuntimeConfig to a bufpluginconfig.MavenRuntimeConfig.
func MavenProtoRuntimeConfigToRuntimeConfig(proto *registryv1alpha1.MavenConfig_RuntimeConfig) (bufpluginconfig.MavenRuntimeConfig, error) {
	libraries := proto.GetRuntimeLibraries()
	var dependencies []bufpluginconfig.MavenDependencyConfig
	for _, library := range libraries {
		dependencies = append(dependencies, ProtoMavenRuntimeLibraryToDependencyConfig(library))
	}
	return bufpluginconfig.MavenRuntimeConfig{
		Name:    proto.GetName(),
		Deps:    dependencies,
		Options: proto.GetOptions(),
	}, nil
}

// MavenRuntimeConfigToProtoRuntimeConfig converts a bufpluginconfig.MavenRuntimeConfig to a registryv1alpha1.MavenConfig_RuntimeLibrary.
func MavenRuntimeConfigToProtoRuntimeConfig(runtime bufpluginconfig.MavenRuntimeConfig) *registryv1alpha1.MavenConfig_RuntimeConfig {
	var libraries []*registryv1alpha1.MavenConfig_RuntimeLibrary
	for _, dependency := range runtime.Deps {
		libraries = append(libraries, MavenDependencyConfigToProtoRuntimeLibrary(dependency))
	}
	return &registryv1alpha1.MavenConfig_RuntimeConfig{
		Name:             runtime.Name,
		RuntimeLibraries: libraries,
		Options:          runtime.Options,
	}
}

// ProtoMavenRuntimeLibraryToDependencyConfig converts a registryv1alpha1 to a bufpluginconfig.MavenDependencyConfig.
func ProtoMavenRuntimeLibraryToDependencyConfig(proto *registryv1alpha1.MavenConfig_RuntimeLibrary) bufpluginconfig.MavenDependencyConfig {
	return bufpluginconfig.MavenDependencyConfig{
		GroupID:    proto.GetGroupId(),
		ArtifactID: proto.GetArtifactId(),
		Version:    proto.GetVersion(),
		Classifier: proto.GetClassifier(),
		Extension:  proto.GetExtension(),
	}
}

func npmImportStyleToNPMProtoImportStyle(importStyle string) (registryv1alpha1.NPMImportStyle, error) {
	switch importStyle {
	case "commonjs":
		return registryv1alpha1.NPMImportStyle_NPM_IMPORT_STYLE_COMMONJS, nil
	case "module":
		return registryv1alpha1.NPMImportStyle_NPM_IMPORT_STYLE_MODULE, nil
	}
	return 0, fmt.Errorf(`invalid import style %q: must be one of "module" or "commonjs"`, importStyle)
}

func npmProtoImportStyleToNPMImportStyle(importStyle registryv1alpha1.NPMImportStyle) (string, error) {
	switch importStyle {
	case registryv1alpha1.NPMImportStyle_NPM_IMPORT_STYLE_COMMONJS:
		return "commonjs", nil
	case registryv1alpha1.NPMImportStyle_NPM_IMPORT_STYLE_MODULE:
		return "module", nil
	}
	return "", fmt.Errorf("unknown import style: %v", importStyle)
}

// goRuntimeDependencyToProtoGoRuntimeLibrary converts a bufpluginconfig.GoRegistryDependencyConfig to a registryv1alpha1.GoConfig_RuntimeLibrary.
func goRuntimeDependencyToProtoGoRuntimeLibrary(config *bufpluginconfig.GoRegistryDependencyConfig) *registryv1alpha1.GoConfig_RuntimeLibrary {
	return &registryv1alpha1.GoConfig_RuntimeLibrary{
		Module:  config.Module,
		Version: config.Version,
	}
}

// protoGoRuntimeLibraryToGoRuntimeDependency converts a registryv1alpha1.GoConfig_RuntimeLibrary to a bufpluginconfig.GoRegistryDependencyConfig.
func protoGoRuntimeLibraryToGoRuntimeDependency(config *registryv1alpha1.GoConfig_RuntimeLibrary) *bufpluginconfig.GoRegistryDependencyConfig {
	return &bufpluginconfig.GoRegistryDependencyConfig{
		Module:  config.Module,
		Version: config.Version,
	}
}

// npmRuntimeDependencyToProtoNPMRuntimeLibrary converts a bufpluginconfig.NPMRegistryConfig to a registryv1alpha1.NPMConfig_RuntimeLibrary.
func npmRuntimeDependencyToProtoNPMRuntimeLibrary(config *bufpluginconfig.NPMRegistryDependencyConfig) *registryv1alpha1.NPMConfig_RuntimeLibrary {
	return &registryv1alpha1.NPMConfig_RuntimeLibrary{
		Package: config.Package,
		Version: config.Version,
	}
}

// protoNPMRuntimeLibraryToNPMRuntimeDependency converts a registryv1alpha1.NPMConfig_RuntimeLibrary to a bufpluginconfig.NPMRegistryDependencyConfig.
func protoNPMRuntimeLibraryToNPMRuntimeDependency(config *registryv1alpha1.NPMConfig_RuntimeLibrary) *bufpluginconfig.NPMRegistryDependencyConfig {
	return &bufpluginconfig.NPMRegistryDependencyConfig{
		Package: config.Package,
		Version: config.Version,
	}
}

// PluginReferencesToCuratedProtoPluginReferences converts a slice of bufpluginref.PluginReference to a slice of registryv1alpha1.CuratedPluginReference.
func PluginReferencesToCuratedProtoPluginReferences(references []bufpluginref.PluginReference) []*registryv1alpha1.CuratedPluginReference {
	if references == nil {
		return nil
	}
	protoReferences := make([]*registryv1alpha1.CuratedPluginReference, 0, len(references))
	for _, reference := range references {
		protoReferences = append(protoReferences, PluginReferenceToProtoCuratedPluginReference(reference))
	}
	return protoReferences
}

// PluginReferenceToProtoCuratedPluginReference converts a bufpluginref.PluginReference to a registryv1alpha1.CuratedPluginReference.
func PluginReferenceToProtoCuratedPluginReference(reference bufpluginref.PluginReference) *registryv1alpha1.CuratedPluginReference {
	if reference == nil {
		return nil
	}
	return &registryv1alpha1.CuratedPluginReference{
		Owner:    reference.Owner(),
		Name:     reference.Plugin(),
		Version:  reference.Version(),
		Revision: uint32(reference.Revision()),
	}
}

// PluginIdentityToProtoCuratedPluginReference converts a bufpluginref.PluginIdentity to a registryv1alpha1.CuratedPluginReference.
//
// The returned CuratedPluginReference contains no Version/Revision information.
func PluginIdentityToProtoCuratedPluginReference(identity bufpluginref.PluginIdentity) *registryv1alpha1.CuratedPluginReference {
	if identity == nil {
		return nil
	}
	return &registryv1alpha1.CuratedPluginReference{
		Owner: identity.Owner(),
		Name:  identity.Plugin(),
	}
}
