# bash completion for hlmdsrv                              -*- shell-script -*-

__hlmdsrv_debug()
{
    if [[ -n ${BASH_COMP_DEBUG_FILE:-} ]]; then
        echo "$*" >> "${BASH_COMP_DEBUG_FILE}"
    fi
}

# Homebrew on Macs have version 1.3 of bash-completion which doesn't include
# _init_completion. This is a very minimal version of that function.
__hlmdsrv_init_completion()
{
    COMPREPLY=()
    _get_comp_words_by_ref "$@" cur prev words cword
}

__hlmdsrv_index_of_word()
{
    local w word=$1
    shift
    index=0
    for w in "$@"; do
        [[ $w = "$word" ]] && return
        index=$((index+1))
    done
    index=-1
}

__hlmdsrv_contains_word()
{
    local w word=$1; shift
    for w in "$@"; do
        [[ $w = "$word" ]] && return
    done
    return 1
}

__hlmdsrv_handle_go_custom_completion()
{
    __hlmdsrv_debug "${FUNCNAME[0]}: cur is ${cur}, words[*] is ${words[*]}, #words[@] is ${#words[@]}"

    local shellCompDirectiveError=1
    local shellCompDirectiveNoSpace=2
    local shellCompDirectiveNoFileComp=4
    local shellCompDirectiveFilterFileExt=8
    local shellCompDirectiveFilterDirs=16

    local out requestComp lastParam lastChar comp directive args

    # Prepare the command to request completions for the program.
    # Calling ${words[0]} instead of directly hlmdsrv allows handling aliases
    args=("${words[@]:1}")
    # Disable ActiveHelp which is not supported for bash completion v1
    requestComp="HLMDSRV_ACTIVE_HELP=0 ${words[0]} __completeNoDesc ${args[*]}"

    lastParam=${words[$((${#words[@]}-1))]}
    lastChar=${lastParam:$((${#lastParam}-1)):1}
    __hlmdsrv_debug "${FUNCNAME[0]}: lastParam ${lastParam}, lastChar ${lastChar}"

    if [ -z "${cur}" ] && [ "${lastChar}" != "=" ]; then
        # If the last parameter is complete (there is a space following it)
        # We add an extra empty parameter so we can indicate this to the go method.
        __hlmdsrv_debug "${FUNCNAME[0]}: Adding extra empty parameter"
        requestComp="${requestComp} \"\""
    fi

    __hlmdsrv_debug "${FUNCNAME[0]}: calling ${requestComp}"
    # Use eval to handle any environment variables and such
    out=$(eval "${requestComp}" 2>/dev/null)

    # Extract the directive integer at the very end of the output following a colon (:)
    directive=${out##*:}
    # Remove the directive
    out=${out%:*}
    if [ "${directive}" = "${out}" ]; then
        # There is not directive specified
        directive=0
    fi
    __hlmdsrv_debug "${FUNCNAME[0]}: the completion directive is: ${directive}"
    __hlmdsrv_debug "${FUNCNAME[0]}: the completions are: ${out}"

    if [ $((directive & shellCompDirectiveError)) -ne 0 ]; then
        # Error code.  No completion.
        __hlmdsrv_debug "${FUNCNAME[0]}: received error from custom completion go code"
        return
    else
        if [ $((directive & shellCompDirectiveNoSpace)) -ne 0 ]; then
            if [[ $(type -t compopt) = "builtin" ]]; then
                __hlmdsrv_debug "${FUNCNAME[0]}: activating no space"
                compopt -o nospace
            fi
        fi
        if [ $((directive & shellCompDirectiveNoFileComp)) -ne 0 ]; then
            if [[ $(type -t compopt) = "builtin" ]]; then
                __hlmdsrv_debug "${FUNCNAME[0]}: activating no file completion"
                compopt +o default
            fi
        fi
    fi

    if [ $((directive & shellCompDirectiveFilterFileExt)) -ne 0 ]; then
        # File extension filtering
        local fullFilter filter filteringCmd
        # Do not use quotes around the $out variable or else newline
        # characters will be kept.
        for filter in ${out}; do
            fullFilter+="$filter|"
        done

        filteringCmd="_filedir $fullFilter"
        __hlmdsrv_debug "File filtering command: $filteringCmd"
        $filteringCmd
    elif [ $((directive & shellCompDirectiveFilterDirs)) -ne 0 ]; then
        # File completion for directories only
        local subdir
        # Use printf to strip any trailing newline
        subdir=$(printf "%s" "${out}")
        if [ -n "$subdir" ]; then
            __hlmdsrv_debug "Listing directories in $subdir"
            __hlmdsrv_handle_subdirs_in_dir_flag "$subdir"
        else
            __hlmdsrv_debug "Listing directories in ."
            _filedir -d
        fi
    else
        while IFS='' read -r comp; do
            COMPREPLY+=("$comp")
        done < <(compgen -W "${out}" -- "$cur")
    fi
}

__hlmdsrv_handle_reply()
{
    __hlmdsrv_debug "${FUNCNAME[0]}"
    local comp
    case $cur in
        -*)
            if [[ $(type -t compopt) = "builtin" ]]; then
                compopt -o nospace
            fi
            local allflags
            if [ ${#must_have_one_flag[@]} -ne 0 ]; then
                allflags=("${must_have_one_flag[@]}")
            else
                allflags=("${flags[*]} ${two_word_flags[*]}")
            fi
            while IFS='' read -r comp; do
                COMPREPLY+=("$comp")
            done < <(compgen -W "${allflags[*]}" -- "$cur")
            if [[ $(type -t compopt) = "builtin" ]]; then
                [[ "${COMPREPLY[0]}" == *= ]] || compopt +o nospace
            fi

            # complete after --flag=abc
            if [[ $cur == *=* ]]; then
                if [[ $(type -t compopt) = "builtin" ]]; then
                    compopt +o nospace
                fi

                local index flag
                flag="${cur%=*}"
                __hlmdsrv_index_of_word "${flag}" "${flags_with_completion[@]}"
                COMPREPLY=()
                if [[ ${index} -ge 0 ]]; then
                    PREFIX=""
                    cur="${cur#*=}"
                    ${flags_completion[${index}]}
                    if [ -n "${ZSH_VERSION:-}" ]; then
                        # zsh completion needs --flag= prefix
                        eval "COMPREPLY=( \"\${COMPREPLY[@]/#/${flag}=}\" )"
                    fi
                fi
            fi

            if [[ -z "${flag_parsing_disabled}" ]]; then
                # If flag parsing is enabled, we have completed the flags and can return.
                # If flag parsing is disabled, we may not know all (or any) of the flags, so we fallthrough
                # to possibly call handle_go_custom_completion.
                return 0;
            fi
            ;;
    esac

    # check if we are handling a flag with special work handling
    local index
    __hlmdsrv_index_of_word "${prev}" "${flags_with_completion[@]}"
    if [[ ${index} -ge 0 ]]; then
        ${flags_completion[${index}]}
        return
    fi

    # we are parsing a flag and don't have a special handler, no completion
    if [[ ${cur} != "${words[cword]}" ]]; then
        return
    fi

    local completions
    completions=("${commands[@]}")
    if [[ ${#must_have_one_noun[@]} -ne 0 ]]; then
        completions+=("${must_have_one_noun[@]}")
    elif [[ -n "${has_completion_function}" ]]; then
        # if a go completion function is provided, defer to that function
        __hlmdsrv_handle_go_custom_completion
    fi
    if [[ ${#must_have_one_flag[@]} -ne 0 ]]; then
        completions+=("${must_have_one_flag[@]}")
    fi
    while IFS='' read -r comp; do
        COMPREPLY+=("$comp")
    done < <(compgen -W "${completions[*]}" -- "$cur")

    if [[ ${#COMPREPLY[@]} -eq 0 && ${#noun_aliases[@]} -gt 0 && ${#must_have_one_noun[@]} -ne 0 ]]; then
        while IFS='' read -r comp; do
            COMPREPLY+=("$comp")
        done < <(compgen -W "${noun_aliases[*]}" -- "$cur")
    fi

    if [[ ${#COMPREPLY[@]} -eq 0 ]]; then
        if declare -F __hlmdsrv_custom_func >/dev/null; then
            # try command name qualified custom func
            __hlmdsrv_custom_func
        else
            # otherwise fall back to unqualified for compatibility
            declare -F __custom_func >/dev/null && __custom_func
        fi
    fi

    # available in bash-completion >= 2, not always present on macOS
    if declare -F __ltrim_colon_completions >/dev/null; then
        __ltrim_colon_completions "$cur"
    fi

    # If there is only 1 completion and it is a flag with an = it will be completed
    # but we don't want a space after the =
    if [[ "${#COMPREPLY[@]}" -eq "1" ]] && [[ $(type -t compopt) = "builtin" ]] && [[ "${COMPREPLY[0]}" == --*= ]]; then
       compopt -o nospace
    fi
}

# The arguments should be in the form "ext1|ext2|extn"
__hlmdsrv_handle_filename_extension_flag()
{
    local ext="$1"
    _filedir "@(${ext})"
}

__hlmdsrv_handle_subdirs_in_dir_flag()
{
    local dir="$1"
    pushd "${dir}" >/dev/null 2>&1 && _filedir -d && popd >/dev/null 2>&1 || return
}

__hlmdsrv_handle_flag()
{
    __hlmdsrv_debug "${FUNCNAME[0]}: c is $c words[c] is ${words[c]}"

    # if a command required a flag, and we found it, unset must_have_one_flag()
    local flagname=${words[c]}
    local flagvalue=""
    # if the word contained an =
    if [[ ${words[c]} == *"="* ]]; then
        flagvalue=${flagname#*=} # take in as flagvalue after the =
        flagname=${flagname%=*} # strip everything after the =
        flagname="${flagname}=" # but put the = back
    fi
    __hlmdsrv_debug "${FUNCNAME[0]}: looking for ${flagname}"
    if __hlmdsrv_contains_word "${flagname}" "${must_have_one_flag[@]}"; then
        must_have_one_flag=()
    fi

    # if you set a flag which only applies to this command, don't show subcommands
    if __hlmdsrv_contains_word "${flagname}" "${local_nonpersistent_flags[@]}"; then
      commands=()
    fi

    # keep flag value with flagname as flaghash
    # flaghash variable is an associative array which is only supported in bash > 3.
    if [[ -z "${BASH_VERSION:-}" || "${BASH_VERSINFO[0]:-}" -gt 3 ]]; then
        if [ -n "${flagvalue}" ] ; then
            flaghash[${flagname}]=${flagvalue}
        elif [ -n "${words[ $((c+1)) ]}" ] ; then
            flaghash[${flagname}]=${words[ $((c+1)) ]}
        else
            flaghash[${flagname}]="true" # pad "true" for bool flag
        fi
    fi

    # skip the argument to a two word flag
    if [[ ${words[c]} != *"="* ]] && __hlmdsrv_contains_word "${words[c]}" "${two_word_flags[@]}"; then
        __hlmdsrv_debug "${FUNCNAME[0]}: found a flag ${words[c]}, skip the next argument"
        c=$((c+1))
        # if we are looking for a flags value, don't show commands
        if [[ $c -eq $cword ]]; then
            commands=()
        fi
    fi

    c=$((c+1))

}

__hlmdsrv_handle_noun()
{
    __hlmdsrv_debug "${FUNCNAME[0]}: c is $c words[c] is ${words[c]}"

    if __hlmdsrv_contains_word "${words[c]}" "${must_have_one_noun[@]}"; then
        must_have_one_noun=()
    elif __hlmdsrv_contains_word "${words[c]}" "${noun_aliases[@]}"; then
        must_have_one_noun=()
    fi

    nouns+=("${words[c]}")
    c=$((c+1))
}

__hlmdsrv_handle_command()
{
    __hlmdsrv_debug "${FUNCNAME[0]}: c is $c words[c] is ${words[c]}"

    local next_command
    if [[ -n ${last_command} ]]; then
        next_command="_${last_command}_${words[c]//:/__}"
    else
        if [[ $c -eq 0 ]]; then
            next_command="_hlmdsrv_root_command"
        else
            next_command="_${words[c]//:/__}"
        fi
    fi
    c=$((c+1))
    __hlmdsrv_debug "${FUNCNAME[0]}: looking for ${next_command}"
    declare -F "$next_command" >/dev/null && $next_command
}

__hlmdsrv_handle_word()
{
    if [[ $c -ge $cword ]]; then
        __hlmdsrv_handle_reply
        return
    fi
    __hlmdsrv_debug "${FUNCNAME[0]}: c is $c words[c] is ${words[c]}"
    if [[ "${words[c]}" == -* ]]; then
        __hlmdsrv_handle_flag
    elif __hlmdsrv_contains_word "${words[c]}" "${commands[@]}"; then
        __hlmdsrv_handle_command
    elif [[ $c -eq 0 ]]; then
        __hlmdsrv_handle_command
    elif __hlmdsrv_contains_word "${words[c]}" "${command_aliases[@]}"; then
        # aliashash variable is an associative array which is only supported in bash > 3.
        if [[ -z "${BASH_VERSION:-}" || "${BASH_VERSINFO[0]:-}" -gt 3 ]]; then
            words[c]=${aliashash[${words[c]}]}
            __hlmdsrv_handle_command
        else
            __hlmdsrv_handle_noun
        fi
    else
        __hlmdsrv_handle_noun
    fi
    __hlmdsrv_handle_word
}

_hlmdsrv_analyze_angle()
{
    last_command="hlmdsrv_analyze_angle"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--a=")
    two_word_flags+=("--a")
    local_nonpersistent_flags+=("--a")
    local_nonpersistent_flags+=("--a=")
    flags+=("--b=")
    two_word_flags+=("--b")
    local_nonpersistent_flags+=("--b")
    local_nonpersistent_flags+=("--b=")
    flags+=("--backend=")
    two_word_flags+=("--backend")
    local_nonpersistent_flags+=("--backend")
    local_nonpersistent_flags+=("--backend=")
    flags+=("--c=")
    two_word_flags+=("--c")
    local_nonpersistent_flags+=("--c")
    local_nonpersistent_flags+=("--c=")
    flags+=("--cutoff=")
    two_word_flags+=("--cutoff")
    local_nonpersistent_flags+=("--cutoff")
    local_nonpersistent_flags+=("--cutoff=")
    flags+=("--d=")
    two_word_flags+=("--d")
    local_nonpersistent_flags+=("--d")
    local_nonpersistent_flags+=("--d=")
    flags+=("--format=")
    two_word_flags+=("--format")
    local_nonpersistent_flags+=("--format")
    local_nonpersistent_flags+=("--format=")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--id=")
    two_word_flags+=("--id")
    local_nonpersistent_flags+=("--id")
    local_nonpersistent_flags+=("--id=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--record")
    local_nonpersistent_flags+=("--record")
    flags+=("--reference-frame=")
    two_word_flags+=("--reference-frame")
    local_nonpersistent_flags+=("--reference-frame")
    local_nonpersistent_flags+=("--reference-frame=")
    flags+=("--selection=")
    two_word_flags+=("--selection")
    local_nonpersistent_flags+=("--selection")
    local_nonpersistent_flags+=("--selection=")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_analyze_contacts()
{
    last_command="hlmdsrv_analyze_contacts"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--a=")
    two_word_flags+=("--a")
    local_nonpersistent_flags+=("--a")
    local_nonpersistent_flags+=("--a=")
    flags+=("--b=")
    two_word_flags+=("--b")
    local_nonpersistent_flags+=("--b")
    local_nonpersistent_flags+=("--b=")
    flags+=("--backend=")
    two_word_flags+=("--backend")
    local_nonpersistent_flags+=("--backend")
    local_nonpersistent_flags+=("--backend=")
    flags+=("--c=")
    two_word_flags+=("--c")
    local_nonpersistent_flags+=("--c")
    local_nonpersistent_flags+=("--c=")
    flags+=("--cutoff=")
    two_word_flags+=("--cutoff")
    local_nonpersistent_flags+=("--cutoff")
    local_nonpersistent_flags+=("--cutoff=")
    flags+=("--d=")
    two_word_flags+=("--d")
    local_nonpersistent_flags+=("--d")
    local_nonpersistent_flags+=("--d=")
    flags+=("--format=")
    two_word_flags+=("--format")
    local_nonpersistent_flags+=("--format")
    local_nonpersistent_flags+=("--format=")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--id=")
    two_word_flags+=("--id")
    local_nonpersistent_flags+=("--id")
    local_nonpersistent_flags+=("--id=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--record")
    local_nonpersistent_flags+=("--record")
    flags+=("--reference-frame=")
    two_word_flags+=("--reference-frame")
    local_nonpersistent_flags+=("--reference-frame")
    local_nonpersistent_flags+=("--reference-frame=")
    flags+=("--selection=")
    two_word_flags+=("--selection")
    local_nonpersistent_flags+=("--selection")
    local_nonpersistent_flags+=("--selection=")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_analyze_dihedral()
{
    last_command="hlmdsrv_analyze_dihedral"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--a=")
    two_word_flags+=("--a")
    local_nonpersistent_flags+=("--a")
    local_nonpersistent_flags+=("--a=")
    flags+=("--b=")
    two_word_flags+=("--b")
    local_nonpersistent_flags+=("--b")
    local_nonpersistent_flags+=("--b=")
    flags+=("--backend=")
    two_word_flags+=("--backend")
    local_nonpersistent_flags+=("--backend")
    local_nonpersistent_flags+=("--backend=")
    flags+=("--c=")
    two_word_flags+=("--c")
    local_nonpersistent_flags+=("--c")
    local_nonpersistent_flags+=("--c=")
    flags+=("--cutoff=")
    two_word_flags+=("--cutoff")
    local_nonpersistent_flags+=("--cutoff")
    local_nonpersistent_flags+=("--cutoff=")
    flags+=("--d=")
    two_word_flags+=("--d")
    local_nonpersistent_flags+=("--d")
    local_nonpersistent_flags+=("--d=")
    flags+=("--format=")
    two_word_flags+=("--format")
    local_nonpersistent_flags+=("--format")
    local_nonpersistent_flags+=("--format=")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--id=")
    two_word_flags+=("--id")
    local_nonpersistent_flags+=("--id")
    local_nonpersistent_flags+=("--id=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--record")
    local_nonpersistent_flags+=("--record")
    flags+=("--reference-frame=")
    two_word_flags+=("--reference-frame")
    local_nonpersistent_flags+=("--reference-frame")
    local_nonpersistent_flags+=("--reference-frame=")
    flags+=("--selection=")
    two_word_flags+=("--selection")
    local_nonpersistent_flags+=("--selection")
    local_nonpersistent_flags+=("--selection=")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_analyze_distance()
{
    last_command="hlmdsrv_analyze_distance"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--a=")
    two_word_flags+=("--a")
    local_nonpersistent_flags+=("--a")
    local_nonpersistent_flags+=("--a=")
    flags+=("--b=")
    two_word_flags+=("--b")
    local_nonpersistent_flags+=("--b")
    local_nonpersistent_flags+=("--b=")
    flags+=("--backend=")
    two_word_flags+=("--backend")
    local_nonpersistent_flags+=("--backend")
    local_nonpersistent_flags+=("--backend=")
    flags+=("--c=")
    two_word_flags+=("--c")
    local_nonpersistent_flags+=("--c")
    local_nonpersistent_flags+=("--c=")
    flags+=("--cutoff=")
    two_word_flags+=("--cutoff")
    local_nonpersistent_flags+=("--cutoff")
    local_nonpersistent_flags+=("--cutoff=")
    flags+=("--d=")
    two_word_flags+=("--d")
    local_nonpersistent_flags+=("--d")
    local_nonpersistent_flags+=("--d=")
    flags+=("--format=")
    two_word_flags+=("--format")
    local_nonpersistent_flags+=("--format")
    local_nonpersistent_flags+=("--format=")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--id=")
    two_word_flags+=("--id")
    local_nonpersistent_flags+=("--id")
    local_nonpersistent_flags+=("--id=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--record")
    local_nonpersistent_flags+=("--record")
    flags+=("--reference-frame=")
    two_word_flags+=("--reference-frame")
    local_nonpersistent_flags+=("--reference-frame")
    local_nonpersistent_flags+=("--reference-frame=")
    flags+=("--selection=")
    two_word_flags+=("--selection")
    local_nonpersistent_flags+=("--selection")
    local_nonpersistent_flags+=("--selection=")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_analyze_hbonds()
{
    last_command="hlmdsrv_analyze_hbonds"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--a=")
    two_word_flags+=("--a")
    local_nonpersistent_flags+=("--a")
    local_nonpersistent_flags+=("--a=")
    flags+=("--b=")
    two_word_flags+=("--b")
    local_nonpersistent_flags+=("--b")
    local_nonpersistent_flags+=("--b=")
    flags+=("--backend=")
    two_word_flags+=("--backend")
    local_nonpersistent_flags+=("--backend")
    local_nonpersistent_flags+=("--backend=")
    flags+=("--c=")
    two_word_flags+=("--c")
    local_nonpersistent_flags+=("--c")
    local_nonpersistent_flags+=("--c=")
    flags+=("--cutoff=")
    two_word_flags+=("--cutoff")
    local_nonpersistent_flags+=("--cutoff")
    local_nonpersistent_flags+=("--cutoff=")
    flags+=("--d=")
    two_word_flags+=("--d")
    local_nonpersistent_flags+=("--d")
    local_nonpersistent_flags+=("--d=")
    flags+=("--format=")
    two_word_flags+=("--format")
    local_nonpersistent_flags+=("--format")
    local_nonpersistent_flags+=("--format=")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--id=")
    two_word_flags+=("--id")
    local_nonpersistent_flags+=("--id")
    local_nonpersistent_flags+=("--id=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--record")
    local_nonpersistent_flags+=("--record")
    flags+=("--reference-frame=")
    two_word_flags+=("--reference-frame")
    local_nonpersistent_flags+=("--reference-frame")
    local_nonpersistent_flags+=("--reference-frame=")
    flags+=("--selection=")
    two_word_flags+=("--selection")
    local_nonpersistent_flags+=("--selection")
    local_nonpersistent_flags+=("--selection=")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_analyze_rgyr()
{
    last_command="hlmdsrv_analyze_rgyr"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--a=")
    two_word_flags+=("--a")
    local_nonpersistent_flags+=("--a")
    local_nonpersistent_flags+=("--a=")
    flags+=("--b=")
    two_word_flags+=("--b")
    local_nonpersistent_flags+=("--b")
    local_nonpersistent_flags+=("--b=")
    flags+=("--backend=")
    two_word_flags+=("--backend")
    local_nonpersistent_flags+=("--backend")
    local_nonpersistent_flags+=("--backend=")
    flags+=("--c=")
    two_word_flags+=("--c")
    local_nonpersistent_flags+=("--c")
    local_nonpersistent_flags+=("--c=")
    flags+=("--cutoff=")
    two_word_flags+=("--cutoff")
    local_nonpersistent_flags+=("--cutoff")
    local_nonpersistent_flags+=("--cutoff=")
    flags+=("--d=")
    two_word_flags+=("--d")
    local_nonpersistent_flags+=("--d")
    local_nonpersistent_flags+=("--d=")
    flags+=("--format=")
    two_word_flags+=("--format")
    local_nonpersistent_flags+=("--format")
    local_nonpersistent_flags+=("--format=")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--id=")
    two_word_flags+=("--id")
    local_nonpersistent_flags+=("--id")
    local_nonpersistent_flags+=("--id=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--record")
    local_nonpersistent_flags+=("--record")
    flags+=("--reference-frame=")
    two_word_flags+=("--reference-frame")
    local_nonpersistent_flags+=("--reference-frame")
    local_nonpersistent_flags+=("--reference-frame=")
    flags+=("--selection=")
    two_word_flags+=("--selection")
    local_nonpersistent_flags+=("--selection")
    local_nonpersistent_flags+=("--selection=")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_analyze_rmsd()
{
    last_command="hlmdsrv_analyze_rmsd"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--a=")
    two_word_flags+=("--a")
    local_nonpersistent_flags+=("--a")
    local_nonpersistent_flags+=("--a=")
    flags+=("--b=")
    two_word_flags+=("--b")
    local_nonpersistent_flags+=("--b")
    local_nonpersistent_flags+=("--b=")
    flags+=("--backend=")
    two_word_flags+=("--backend")
    local_nonpersistent_flags+=("--backend")
    local_nonpersistent_flags+=("--backend=")
    flags+=("--c=")
    two_word_flags+=("--c")
    local_nonpersistent_flags+=("--c")
    local_nonpersistent_flags+=("--c=")
    flags+=("--cutoff=")
    two_word_flags+=("--cutoff")
    local_nonpersistent_flags+=("--cutoff")
    local_nonpersistent_flags+=("--cutoff=")
    flags+=("--d=")
    two_word_flags+=("--d")
    local_nonpersistent_flags+=("--d")
    local_nonpersistent_flags+=("--d=")
    flags+=("--format=")
    two_word_flags+=("--format")
    local_nonpersistent_flags+=("--format")
    local_nonpersistent_flags+=("--format=")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--id=")
    two_word_flags+=("--id")
    local_nonpersistent_flags+=("--id")
    local_nonpersistent_flags+=("--id=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--record")
    local_nonpersistent_flags+=("--record")
    flags+=("--reference-frame=")
    two_word_flags+=("--reference-frame")
    local_nonpersistent_flags+=("--reference-frame")
    local_nonpersistent_flags+=("--reference-frame=")
    flags+=("--selection=")
    two_word_flags+=("--selection")
    local_nonpersistent_flags+=("--selection")
    local_nonpersistent_flags+=("--selection=")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_analyze_rmsf()
{
    last_command="hlmdsrv_analyze_rmsf"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--a=")
    two_word_flags+=("--a")
    local_nonpersistent_flags+=("--a")
    local_nonpersistent_flags+=("--a=")
    flags+=("--b=")
    two_word_flags+=("--b")
    local_nonpersistent_flags+=("--b")
    local_nonpersistent_flags+=("--b=")
    flags+=("--backend=")
    two_word_flags+=("--backend")
    local_nonpersistent_flags+=("--backend")
    local_nonpersistent_flags+=("--backend=")
    flags+=("--c=")
    two_word_flags+=("--c")
    local_nonpersistent_flags+=("--c")
    local_nonpersistent_flags+=("--c=")
    flags+=("--cutoff=")
    two_word_flags+=("--cutoff")
    local_nonpersistent_flags+=("--cutoff")
    local_nonpersistent_flags+=("--cutoff=")
    flags+=("--d=")
    two_word_flags+=("--d")
    local_nonpersistent_flags+=("--d")
    local_nonpersistent_flags+=("--d=")
    flags+=("--format=")
    two_word_flags+=("--format")
    local_nonpersistent_flags+=("--format")
    local_nonpersistent_flags+=("--format=")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--id=")
    two_word_flags+=("--id")
    local_nonpersistent_flags+=("--id")
    local_nonpersistent_flags+=("--id=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--record")
    local_nonpersistent_flags+=("--record")
    flags+=("--reference-frame=")
    two_word_flags+=("--reference-frame")
    local_nonpersistent_flags+=("--reference-frame")
    local_nonpersistent_flags+=("--reference-frame=")
    flags+=("--selection=")
    two_word_flags+=("--selection")
    local_nonpersistent_flags+=("--selection")
    local_nonpersistent_flags+=("--selection=")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_analyze_sasa()
{
    last_command="hlmdsrv_analyze_sasa"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--a=")
    two_word_flags+=("--a")
    local_nonpersistent_flags+=("--a")
    local_nonpersistent_flags+=("--a=")
    flags+=("--b=")
    two_word_flags+=("--b")
    local_nonpersistent_flags+=("--b")
    local_nonpersistent_flags+=("--b=")
    flags+=("--backend=")
    two_word_flags+=("--backend")
    local_nonpersistent_flags+=("--backend")
    local_nonpersistent_flags+=("--backend=")
    flags+=("--c=")
    two_word_flags+=("--c")
    local_nonpersistent_flags+=("--c")
    local_nonpersistent_flags+=("--c=")
    flags+=("--cutoff=")
    two_word_flags+=("--cutoff")
    local_nonpersistent_flags+=("--cutoff")
    local_nonpersistent_flags+=("--cutoff=")
    flags+=("--d=")
    two_word_flags+=("--d")
    local_nonpersistent_flags+=("--d")
    local_nonpersistent_flags+=("--d=")
    flags+=("--format=")
    two_word_flags+=("--format")
    local_nonpersistent_flags+=("--format")
    local_nonpersistent_flags+=("--format=")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--id=")
    two_word_flags+=("--id")
    local_nonpersistent_flags+=("--id")
    local_nonpersistent_flags+=("--id=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--record")
    local_nonpersistent_flags+=("--record")
    flags+=("--reference-frame=")
    two_word_flags+=("--reference-frame")
    local_nonpersistent_flags+=("--reference-frame")
    local_nonpersistent_flags+=("--reference-frame=")
    flags+=("--selection=")
    two_word_flags+=("--selection")
    local_nonpersistent_flags+=("--selection")
    local_nonpersistent_flags+=("--selection=")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_analyze()
{
    last_command="hlmdsrv_analyze"

    command_aliases=()

    commands=()
    commands+=("angle")
    commands+=("contacts")
    commands+=("dihedral")
    commands+=("distance")
    commands+=("hbonds")
    commands+=("rgyr")
    commands+=("rmsd")
    commands+=("rmsf")
    commands+=("sasa")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_batch()
{
    last_command="hlmdsrv_batch"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--concurrency=")
    two_word_flags+=("--concurrency")
    local_nonpersistent_flags+=("--concurrency")
    local_nonpersistent_flags+=("--concurrency=")
    flags+=("--continue-on-error")
    local_nonpersistent_flags+=("--continue-on-error")
    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_bench()
{
    last_command="hlmdsrv_bench"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--atoms=")
    two_word_flags+=("--atoms")
    local_nonpersistent_flags+=("--atoms")
    local_nonpersistent_flags+=("--atoms=")
    flags+=("--frames=")
    two_word_flags+=("--frames")
    local_nonpersistent_flags+=("--frames")
    local_nonpersistent_flags+=("--frames=")
    flags+=("--iterations=")
    two_word_flags+=("--iterations")
    local_nonpersistent_flags+=("--iterations")
    local_nonpersistent_flags+=("--iterations=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--out=")
    two_word_flags+=("--out")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_capabilities()
{
    last_command="hlmdsrv_capabilities"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_compat_check()
{
    last_command="hlmdsrv_compat_check"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--docker")
    local_nonpersistent_flags+=("--docker")
    flags+=("--image=")
    two_word_flags+=("--image")
    local_nonpersistent_flags+=("--image")
    local_nonpersistent_flags+=("--image=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--port=")
    two_word_flags+=("--port")
    local_nonpersistent_flags+=("--port")
    local_nonpersistent_flags+=("--port=")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")
    flags+=("--profile=")
    two_word_flags+=("--profile")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_compat()
{
    last_command="hlmdsrv_compat"

    command_aliases=()

    commands=()
    commands+=("check")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_completion()
{
    last_command="hlmdsrv_completion"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_config_init()
{
    last_command="hlmdsrv_config_init"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--auth-token=")
    two_word_flags+=("--auth-token")
    local_nonpersistent_flags+=("--auth-token")
    local_nonpersistent_flags+=("--auth-token=")
    flags+=("--backend=")
    two_word_flags+=("--backend")
    local_nonpersistent_flags+=("--backend")
    local_nonpersistent_flags+=("--backend=")
    flags+=("--cache=")
    two_word_flags+=("--cache")
    local_nonpersistent_flags+=("--cache")
    local_nonpersistent_flags+=("--cache=")
    flags+=("--config=")
    two_word_flags+=("--config")
    local_nonpersistent_flags+=("--config")
    local_nonpersistent_flags+=("--config=")
    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--job-prune-on-start")
    local_nonpersistent_flags+=("--job-prune-on-start")
    flags+=("--job-ttl=")
    two_word_flags+=("--job-ttl")
    local_nonpersistent_flags+=("--job-ttl")
    local_nonpersistent_flags+=("--job-ttl=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    local_nonpersistent_flags+=("--profile")
    local_nonpersistent_flags+=("--profile=")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_config_list()
{
    last_command="hlmdsrv_config_list"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    local_nonpersistent_flags+=("--config")
    local_nonpersistent_flags+=("--config=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_config_path()
{
    last_command="hlmdsrv_config_path"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    local_nonpersistent_flags+=("--config")
    local_nonpersistent_flags+=("--config=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_config()
{
    last_command="hlmdsrv_config"

    command_aliases=()

    commands=()
    commands+=("init")
    commands+=("list")
    commands+=("path")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_dataset_delete()
{
    last_command="hlmdsrv_dataset_delete"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--files")
    local_nonpersistent_flags+=("--files")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_dataset_gc()
{
    last_command="hlmdsrv_dataset_gc"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_dataset_inspect()
{
    last_command="hlmdsrv_dataset_inspect"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--backend=")
    two_word_flags+=("--backend")
    local_nonpersistent_flags+=("--backend")
    local_nonpersistent_flags+=("--backend=")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_dataset_rename()
{
    last_command="hlmdsrv_dataset_rename"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_dataset_update()
{
    last_command="hlmdsrv_dataset_update"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--description=")
    two_word_flags+=("--description")
    local_nonpersistent_flags+=("--description")
    local_nonpersistent_flags+=("--description=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--license=")
    two_word_flags+=("--license")
    local_nonpersistent_flags+=("--license")
    local_nonpersistent_flags+=("--license=")
    flags+=("--name=")
    two_word_flags+=("--name")
    local_nonpersistent_flags+=("--name")
    local_nonpersistent_flags+=("--name=")
    flags+=("--source=")
    two_word_flags+=("--source")
    local_nonpersistent_flags+=("--source")
    local_nonpersistent_flags+=("--source=")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_dataset()
{
    last_command="hlmdsrv_dataset"

    command_aliases=()

    commands=()
    commands+=("delete")
    commands+=("gc")
    commands+=("inspect")
    commands+=("rename")
    commands+=("update")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_debug_bundle()
{
    last_command="hlmdsrv_debug_bundle"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--backend=")
    two_word_flags+=("--backend")
    local_nonpersistent_flags+=("--backend")
    local_nonpersistent_flags+=("--backend=")
    flags+=("--deep")
    local_nonpersistent_flags+=("--deep")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--log-bytes=")
    two_word_flags+=("--log-bytes")
    local_nonpersistent_flags+=("--log-bytes")
    local_nonpersistent_flags+=("--log-bytes=")
    flags+=("--max-file-bytes=")
    two_word_flags+=("--max-file-bytes")
    local_nonpersistent_flags+=("--max-file-bytes")
    local_nonpersistent_flags+=("--max-file-bytes=")
    flags+=("--max-logs=")
    two_word_flags+=("--max-logs")
    local_nonpersistent_flags+=("--max-logs")
    local_nonpersistent_flags+=("--max-logs=")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--skip-smoke")
    local_nonpersistent_flags+=("--skip-smoke")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--strict")
    local_nonpersistent_flags+=("--strict")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_debug()
{
    last_command="hlmdsrv_debug"

    command_aliases=()

    commands=()
    commands+=("bundle")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_demo_create()
{
    last_command="hlmdsrv_demo_create"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--frames=")
    two_word_flags+=("--frames")
    local_nonpersistent_flags+=("--frames")
    local_nonpersistent_flags+=("--frames=")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--id=")
    two_word_flags+=("--id")
    local_nonpersistent_flags+=("--id")
    local_nonpersistent_flags+=("--id=")
    flags+=("--job=")
    two_word_flags+=("--job")
    local_nonpersistent_flags+=("--job")
    local_nonpersistent_flags+=("--job=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--name=")
    two_word_flags+=("--name")
    local_nonpersistent_flags+=("--name")
    local_nonpersistent_flags+=("--name=")
    flags+=("--out=")
    two_word_flags+=("--out")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_demo_gromacs()
{
    last_command="hlmdsrv_demo_gromacs"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--frames=")
    two_word_flags+=("--frames")
    local_nonpersistent_flags+=("--frames")
    local_nonpersistent_flags+=("--frames=")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--id=")
    two_word_flags+=("--id")
    local_nonpersistent_flags+=("--id")
    local_nonpersistent_flags+=("--id=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--name=")
    two_word_flags+=("--name")
    local_nonpersistent_flags+=("--name")
    local_nonpersistent_flags+=("--name=")
    flags+=("--out=")
    two_word_flags+=("--out")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_demo()
{
    last_command="hlmdsrv_demo"

    command_aliases=()

    commands=()
    commands+=("create")
    commands+=("gromacs")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_docs()
{
    last_command="hlmdsrv_docs"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--out=")
    two_word_flags+=("--out")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_doctor()
{
    last_command="hlmdsrv_doctor"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--cache=")
    two_word_flags+=("--cache")
    local_nonpersistent_flags+=("--cache")
    local_nonpersistent_flags+=("--cache=")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--static-out=")
    two_word_flags+=("--static-out")
    local_nonpersistent_flags+=("--static-out")
    local_nonpersistent_flags+=("--static-out=")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--strict")
    local_nonpersistent_flags+=("--strict")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_explain()
{
    last_command="hlmdsrv_explain"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--backend=")
    two_word_flags+=("--backend")
    local_nonpersistent_flags+=("--backend")
    local_nonpersistent_flags+=("--backend=")
    flags+=("--cache=")
    two_word_flags+=("--cache")
    local_nonpersistent_flags+=("--cache")
    local_nonpersistent_flags+=("--cache=")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--strict")
    local_nonpersistent_flags+=("--strict")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_export()
{
    last_command="hlmdsrv_export"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--backend=")
    two_word_flags+=("--backend")
    local_nonpersistent_flags+=("--backend")
    local_nonpersistent_flags+=("--backend=")
    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--format=")
    two_word_flags+=("--format")
    local_nonpersistent_flags+=("--format")
    local_nonpersistent_flags+=("--format=")
    flags+=("--frames=")
    two_word_flags+=("--frames")
    local_nonpersistent_flags+=("--frames")
    local_nonpersistent_flags+=("--frames=")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--group=")
    two_word_flags+=("--group")
    local_nonpersistent_flags+=("--group")
    local_nonpersistent_flags+=("--group=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--selection=")
    two_word_flags+=("--selection")
    local_nonpersistent_flags+=("--selection")
    local_nonpersistent_flags+=("--selection=")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_fixtures_list()
{
    last_command="hlmdsrv_fixtures_list"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_fixtures_mdanalysis-adk()
{
    last_command="hlmdsrv_fixtures_mdanalysis-adk"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--id=")
    two_word_flags+=("--id")
    local_nonpersistent_flags+=("--id")
    local_nonpersistent_flags+=("--id=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--probe")
    local_nonpersistent_flags+=("--probe")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_fixtures()
{
    last_command="hlmdsrv_fixtures"

    command_aliases=()

    commands=()
    commands+=("list")
    commands+=("mdanalysis-adk")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_frames_count()
{
    last_command="hlmdsrv_frames_count"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--backend=")
    two_word_flags+=("--backend")
    local_nonpersistent_flags+=("--backend")
    local_nonpersistent_flags+=("--backend=")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_frames_extract()
{
    last_command="hlmdsrv_frames_extract"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--backend=")
    two_word_flags+=("--backend")
    local_nonpersistent_flags+=("--backend")
    local_nonpersistent_flags+=("--backend=")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_frames_get()
{
    last_command="hlmdsrv_frames_get"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--atom-subset=")
    two_word_flags+=("--atom-subset")
    local_nonpersistent_flags+=("--atom-subset")
    local_nonpersistent_flags+=("--atom-subset=")
    flags+=("--backend=")
    two_word_flags+=("--backend")
    local_nonpersistent_flags+=("--backend")
    local_nonpersistent_flags+=("--backend=")
    flags+=("--format=")
    two_word_flags+=("--format")
    local_nonpersistent_flags+=("--format")
    local_nonpersistent_flags+=("--format=")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_frames()
{
    last_command="hlmdsrv_frames"

    command_aliases=()

    commands=()
    commands+=("count")
    commands+=("extract")
    commands+=("get")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_gromacs_convert()
{
    last_command="hlmdsrv_gromacs_convert"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--command=")
    two_word_flags+=("--command")
    local_nonpersistent_flags+=("--command")
    local_nonpersistent_flags+=("--command=")
    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_gromacs_doctor()
{
    last_command="hlmdsrv_gromacs_doctor"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--command=")
    two_word_flags+=("--command")
    local_nonpersistent_flags+=("--command")
    local_nonpersistent_flags+=("--command=")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_gromacs_extract()
{
    last_command="hlmdsrv_gromacs_extract"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--command=")
    two_word_flags+=("--command")
    local_nonpersistent_flags+=("--command")
    local_nonpersistent_flags+=("--command=")
    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--frame=")
    two_word_flags+=("--frame")
    local_nonpersistent_flags+=("--frame")
    local_nonpersistent_flags+=("--frame=")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--time=")
    two_word_flags+=("--time")
    local_nonpersistent_flags+=("--time")
    local_nonpersistent_flags+=("--time=")
    flags+=("--topology=")
    two_word_flags+=("--topology")
    local_nonpersistent_flags+=("--topology")
    local_nonpersistent_flags+=("--topology=")
    flags+=("--trajectory=")
    two_word_flags+=("--trajectory")
    local_nonpersistent_flags+=("--trajectory")
    local_nonpersistent_flags+=("--trajectory=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_gromacs_probe()
{
    last_command="hlmdsrv_gromacs_probe"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--command=")
    two_word_flags+=("--command")
    local_nonpersistent_flags+=("--command")
    local_nonpersistent_flags+=("--command=")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_gromacs()
{
    last_command="hlmdsrv_gromacs"

    command_aliases=()

    commands=()
    commands+=("convert")
    commands+=("doctor")
    commands+=("extract")
    commands+=("probe")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_index_build()
{
    last_command="hlmdsrv_index_build"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--chunk-size=")
    two_word_flags+=("--chunk-size")
    local_nonpersistent_flags+=("--chunk-size")
    local_nonpersistent_flags+=("--chunk-size=")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--max-atoms=")
    two_word_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms=")
    flags+=("--max-frames=")
    two_word_flags+=("--max-frames")
    local_nonpersistent_flags+=("--max-frames")
    local_nonpersistent_flags+=("--max-frames=")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_index_chunks()
{
    last_command="hlmdsrv_index_chunks"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--chunk-size=")
    two_word_flags+=("--chunk-size")
    local_nonpersistent_flags+=("--chunk-size")
    local_nonpersistent_flags+=("--chunk-size=")
    flags+=("--encoding=")
    two_word_flags+=("--encoding")
    local_nonpersistent_flags+=("--encoding")
    local_nonpersistent_flags+=("--encoding=")
    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--max-atoms=")
    two_word_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms=")
    flags+=("--max-chunk-bytes=")
    two_word_flags+=("--max-chunk-bytes")
    local_nonpersistent_flags+=("--max-chunk-bytes")
    local_nonpersistent_flags+=("--max-chunk-bytes=")
    flags+=("--max-frames=")
    two_word_flags+=("--max-frames")
    local_nonpersistent_flags+=("--max-frames")
    local_nonpersistent_flags+=("--max-frames=")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_index_show()
{
    last_command="hlmdsrv_index_show"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_index()
{
    last_command="hlmdsrv_index"

    command_aliases=()

    commands=()
    commands+=("build")
    commands+=("chunks")
    commands+=("show")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_ingest()
{
    last_command="hlmdsrv_ingest"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--atom-subset=")
    two_word_flags+=("--atom-subset")
    local_nonpersistent_flags+=("--atom-subset")
    local_nonpersistent_flags+=("--atom-subset=")
    flags+=("--cache=")
    two_word_flags+=("--cache")
    local_nonpersistent_flags+=("--cache")
    local_nonpersistent_flags+=("--cache=")
    flags+=("--coordinate-unit=")
    two_word_flags+=("--coordinate-unit")
    local_nonpersistent_flags+=("--coordinate-unit")
    local_nonpersistent_flags+=("--coordinate-unit=")
    flags+=("--created-by=")
    two_word_flags+=("--created-by")
    local_nonpersistent_flags+=("--created-by")
    local_nonpersistent_flags+=("--created-by=")
    flags+=("--description=")
    two_word_flags+=("--description")
    local_nonpersistent_flags+=("--description")
    local_nonpersistent_flags+=("--description=")
    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--id=")
    two_word_flags+=("--id")
    local_nonpersistent_flags+=("--id")
    local_nonpersistent_flags+=("--id=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--license=")
    two_word_flags+=("--license")
    local_nonpersistent_flags+=("--license")
    local_nonpersistent_flags+=("--license=")
    flags+=("--max-atoms=")
    two_word_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms=")
    flags+=("--max-frames=")
    two_word_flags+=("--max-frames")
    local_nonpersistent_flags+=("--max-frames")
    local_nonpersistent_flags+=("--max-frames=")
    flags+=("--name=")
    two_word_flags+=("--name")
    local_nonpersistent_flags+=("--name")
    local_nonpersistent_flags+=("--name=")
    flags+=("--probe")
    local_nonpersistent_flags+=("--probe")
    flags+=("--source=")
    two_word_flags+=("--source")
    local_nonpersistent_flags+=("--source")
    local_nonpersistent_flags+=("--source=")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--stride=")
    two_word_flags+=("--stride")
    local_nonpersistent_flags+=("--stride")
    local_nonpersistent_flags+=("--stride=")
    flags+=("--time-unit=")
    two_word_flags+=("--time-unit")
    local_nonpersistent_flags+=("--time-unit")
    local_nonpersistent_flags+=("--time-unit=")
    flags+=("--topology=")
    two_word_flags+=("--topology")
    local_nonpersistent_flags+=("--topology")
    local_nonpersistent_flags+=("--topology=")
    flags+=("--topology-url=")
    two_word_flags+=("--topology-url")
    local_nonpersistent_flags+=("--topology-url")
    local_nonpersistent_flags+=("--topology-url=")
    flags+=("--trajectory=")
    two_word_flags+=("--trajectory")
    local_nonpersistent_flags+=("--trajectory")
    local_nonpersistent_flags+=("--trajectory=")
    flags+=("--trajectory-url=")
    two_word_flags+=("--trajectory-url")
    local_nonpersistent_flags+=("--trajectory-url")
    local_nonpersistent_flags+=("--trajectory-url=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_init_job()
{
    last_command="hlmdsrv_init_job"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--chunk-size=")
    two_word_flags+=("--chunk-size")
    local_nonpersistent_flags+=("--chunk-size")
    local_nonpersistent_flags+=("--chunk-size=")
    flags+=("--chunks")
    local_nonpersistent_flags+=("--chunks")
    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--id=")
    two_word_flags+=("--id")
    local_nonpersistent_flags+=("--id")
    local_nonpersistent_flags+=("--id=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--name=")
    two_word_flags+=("--name")
    local_nonpersistent_flags+=("--name")
    local_nonpersistent_flags+=("--name=")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--topology=")
    two_word_flags+=("--topology")
    local_nonpersistent_flags+=("--topology")
    local_nonpersistent_flags+=("--topology=")
    flags+=("--topology-url=")
    two_word_flags+=("--topology-url")
    local_nonpersistent_flags+=("--topology-url")
    local_nonpersistent_flags+=("--topology-url=")
    flags+=("--trajectory=")
    two_word_flags+=("--trajectory")
    local_nonpersistent_flags+=("--trajectory")
    local_nonpersistent_flags+=("--trajectory=")
    flags+=("--trajectory-url=")
    two_word_flags+=("--trajectory-url")
    local_nonpersistent_flags+=("--trajectory-url")
    local_nonpersistent_flags+=("--trajectory-url=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_init()
{
    last_command="hlmdsrv_init"

    command_aliases=()

    commands=()
    commands+=("job")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_install_backends()
{
    last_command="hlmdsrv_install_backends"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_install_completions()
{
    last_command="hlmdsrv_install_completions"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--shell=")
    two_word_flags+=("--shell")
    local_nonpersistent_flags+=("--shell")
    local_nonpersistent_flags+=("--shell=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_install_local()
{
    last_command="hlmdsrv_install_local"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--bin-dir=")
    two_word_flags+=("--bin-dir")
    local_nonpersistent_flags+=("--bin-dir")
    local_nonpersistent_flags+=("--bin-dir=")
    flags+=("--completion-dir=")
    two_word_flags+=("--completion-dir")
    local_nonpersistent_flags+=("--completion-dir")
    local_nonpersistent_flags+=("--completion-dir=")
    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--home=")
    two_word_flags+=("--home")
    local_nonpersistent_flags+=("--home")
    local_nonpersistent_flags+=("--home=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--name=")
    two_word_flags+=("--name")
    local_nonpersistent_flags+=("--name")
    local_nonpersistent_flags+=("--name=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_install()
{
    last_command="hlmdsrv_install"

    command_aliases=()

    commands=()
    commands+=("backends")
    commands+=("completions")
    commands+=("local")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_jobs_cancel()
{
    last_command="hlmdsrv_jobs_cancel"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--server=")
    two_word_flags+=("--server")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--token=")
    two_word_flags+=("--token")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_jobs_events()
{
    last_command="hlmdsrv_jobs_events"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--server=")
    two_word_flags+=("--server")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--token=")
    two_word_flags+=("--token")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_jobs_list()
{
    last_command="hlmdsrv_jobs_list"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--server=")
    two_word_flags+=("--server")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--token=")
    two_word_flags+=("--token")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_jobs_logs()
{
    last_command="hlmdsrv_jobs_logs"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--server=")
    two_word_flags+=("--server")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--token=")
    two_word_flags+=("--token")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_jobs_prune()
{
    last_command="hlmdsrv_jobs_prune"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--dry-run")
    local_nonpersistent_flags+=("--dry-run")
    flags+=("--status=")
    two_word_flags+=("--status")
    local_nonpersistent_flags+=("--status")
    local_nonpersistent_flags+=("--status=")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--ttl=")
    two_word_flags+=("--ttl")
    local_nonpersistent_flags+=("--ttl")
    local_nonpersistent_flags+=("--ttl=")
    flags+=("--json")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--server=")
    two_word_flags+=("--server")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--token=")
    two_word_flags+=("--token")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_jobs_retry()
{
    last_command="hlmdsrv_jobs_retry"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--interval=")
    two_word_flags+=("--interval")
    local_nonpersistent_flags+=("--interval")
    local_nonpersistent_flags+=("--interval=")
    flags+=("--wait")
    local_nonpersistent_flags+=("--wait")
    flags+=("--wait-timeout=")
    two_word_flags+=("--wait-timeout")
    local_nonpersistent_flags+=("--wait-timeout")
    local_nonpersistent_flags+=("--wait-timeout=")
    flags+=("--json")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--server=")
    two_word_flags+=("--server")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--token=")
    two_word_flags+=("--token")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_jobs_stats()
{
    last_command="hlmdsrv_jobs_stats"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--server=")
    two_word_flags+=("--server")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--token=")
    two_word_flags+=("--token")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_jobs_status()
{
    last_command="hlmdsrv_jobs_status"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--server=")
    two_word_flags+=("--server")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--token=")
    two_word_flags+=("--token")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_jobs_submit()
{
    last_command="hlmdsrv_jobs_submit"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--a=")
    two_word_flags+=("--a")
    local_nonpersistent_flags+=("--a")
    local_nonpersistent_flags+=("--a=")
    flags+=("--analysis-id=")
    two_word_flags+=("--analysis-id")
    local_nonpersistent_flags+=("--analysis-id")
    local_nonpersistent_flags+=("--analysis-id=")
    flags+=("--analysis-type=")
    two_word_flags+=("--analysis-type")
    local_nonpersistent_flags+=("--analysis-type")
    local_nonpersistent_flags+=("--analysis-type=")
    flags+=("--b=")
    two_word_flags+=("--b")
    local_nonpersistent_flags+=("--b")
    local_nonpersistent_flags+=("--b=")
    flags+=("--backend=")
    two_word_flags+=("--backend")
    local_nonpersistent_flags+=("--backend")
    local_nonpersistent_flags+=("--backend=")
    flags+=("--c=")
    two_word_flags+=("--c")
    local_nonpersistent_flags+=("--c")
    local_nonpersistent_flags+=("--c=")
    flags+=("--chunk-size=")
    two_word_flags+=("--chunk-size")
    local_nonpersistent_flags+=("--chunk-size")
    local_nonpersistent_flags+=("--chunk-size=")
    flags+=("--d=")
    two_word_flags+=("--d")
    local_nonpersistent_flags+=("--d")
    local_nonpersistent_flags+=("--d=")
    flags+=("--dataset=")
    two_word_flags+=("--dataset")
    local_nonpersistent_flags+=("--dataset")
    local_nonpersistent_flags+=("--dataset=")
    flags+=("--encoding=")
    two_word_flags+=("--encoding")
    local_nonpersistent_flags+=("--encoding")
    local_nonpersistent_flags+=("--encoding=")
    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--format=")
    two_word_flags+=("--format")
    local_nonpersistent_flags+=("--format")
    local_nonpersistent_flags+=("--format=")
    flags+=("--interval=")
    two_word_flags+=("--interval")
    local_nonpersistent_flags+=("--interval")
    local_nonpersistent_flags+=("--interval=")
    flags+=("--out=")
    two_word_flags+=("--out")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    flags+=("--request=")
    two_word_flags+=("--request")
    local_nonpersistent_flags+=("--request")
    local_nonpersistent_flags+=("--request=")
    flags+=("--selection=")
    two_word_flags+=("--selection")
    local_nonpersistent_flags+=("--selection")
    local_nonpersistent_flags+=("--selection=")
    flags+=("--timeout-seconds=")
    two_word_flags+=("--timeout-seconds")
    local_nonpersistent_flags+=("--timeout-seconds")
    local_nonpersistent_flags+=("--timeout-seconds=")
    flags+=("--type=")
    two_word_flags+=("--type")
    local_nonpersistent_flags+=("--type")
    local_nonpersistent_flags+=("--type=")
    flags+=("--wait")
    local_nonpersistent_flags+=("--wait")
    flags+=("--wait-timeout=")
    two_word_flags+=("--wait-timeout")
    local_nonpersistent_flags+=("--wait-timeout")
    local_nonpersistent_flags+=("--wait-timeout=")
    flags+=("--json")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--server=")
    two_word_flags+=("--server")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--token=")
    two_word_flags+=("--token")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_jobs_wait()
{
    last_command="hlmdsrv_jobs_wait"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--interval=")
    two_word_flags+=("--interval")
    local_nonpersistent_flags+=("--interval")
    local_nonpersistent_flags+=("--interval=")
    flags+=("--wait-timeout=")
    two_word_flags+=("--wait-timeout")
    local_nonpersistent_flags+=("--wait-timeout")
    local_nonpersistent_flags+=("--wait-timeout=")
    flags+=("--json")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--server=")
    two_word_flags+=("--server")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    flags+=("--token=")
    two_word_flags+=("--token")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_jobs()
{
    last_command="hlmdsrv_jobs"

    command_aliases=()

    commands=()
    commands+=("cancel")
    commands+=("events")
    commands+=("list")
    commands+=("logs")
    commands+=("prune")
    commands+=("retry")
    commands+=("stats")
    commands+=("status")
    commands+=("submit")
    commands+=("wait")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    flags+=("--server=")
    two_word_flags+=("--server")
    flags+=("--token=")
    two_word_flags+=("--token")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_list_datasets()
{
    last_command="hlmdsrv_list_datasets"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_list()
{
    last_command="hlmdsrv_list"

    command_aliases=()

    commands=()
    commands+=("datasets")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_pack()
{
    last_command="hlmdsrv_pack"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_probe()
{
    last_command="hlmdsrv_probe"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_publish_static()
{
    last_command="hlmdsrv_publish_static"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--verify")
    local_nonpersistent_flags+=("--verify")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_publish()
{
    last_command="hlmdsrv_publish"

    command_aliases=()

    commands=()
    commands+=("static")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_quickstart()
{
    last_command="hlmdsrv_quickstart"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--backend=")
    two_word_flags+=("--backend")
    local_nonpersistent_flags+=("--backend")
    local_nonpersistent_flags+=("--backend=")
    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--frames=")
    two_word_flags+=("--frames")
    local_nonpersistent_flags+=("--frames")
    local_nonpersistent_flags+=("--frames=")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--id=")
    two_word_flags+=("--id")
    local_nonpersistent_flags+=("--id")
    local_nonpersistent_flags+=("--id=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--out=")
    two_word_flags+=("--out")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_run()
{
    last_command="hlmdsrv_run"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--analysis-timeout=")
    two_word_flags+=("--analysis-timeout")
    local_nonpersistent_flags+=("--analysis-timeout")
    local_nonpersistent_flags+=("--analysis-timeout=")
    flags+=("--backend=")
    two_word_flags+=("--backend")
    local_nonpersistent_flags+=("--backend")
    local_nonpersistent_flags+=("--backend=")
    flags+=("--cache=")
    two_word_flags+=("--cache")
    local_nonpersistent_flags+=("--cache")
    local_nonpersistent_flags+=("--cache=")
    flags+=("--chunk-encoding=")
    two_word_flags+=("--chunk-encoding")
    local_nonpersistent_flags+=("--chunk-encoding")
    local_nonpersistent_flags+=("--chunk-encoding=")
    flags+=("--chunks")
    local_nonpersistent_flags+=("--chunks")
    flags+=("--dry-run")
    local_nonpersistent_flags+=("--dry-run")
    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--index")
    local_nonpersistent_flags+=("--index")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--max-atoms=")
    two_word_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms=")
    flags+=("--max-chunk-bytes=")
    two_word_flags+=("--max-chunk-bytes")
    local_nonpersistent_flags+=("--max-chunk-bytes")
    local_nonpersistent_flags+=("--max-chunk-bytes=")
    flags+=("--max-frames=")
    two_word_flags+=("--max-frames")
    local_nonpersistent_flags+=("--max-frames")
    local_nonpersistent_flags+=("--max-frames=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--probe")
    local_nonpersistent_flags+=("--probe")
    flags+=("--probe-timeout=")
    two_word_flags+=("--probe-timeout")
    local_nonpersistent_flags+=("--probe-timeout")
    local_nonpersistent_flags+=("--probe-timeout=")
    flags+=("--report=")
    two_word_flags+=("--report")
    local_nonpersistent_flags+=("--report")
    local_nonpersistent_flags+=("--report=")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--strict")
    local_nonpersistent_flags+=("--strict")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_schema_batch()
{
    last_command="hlmdsrv_schema_batch"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_schema_job()
{
    last_command="hlmdsrv_schema_job"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_schema_manifest()
{
    last_command="hlmdsrv_schema_manifest"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_schema_openapi()
{
    last_command="hlmdsrv_schema_openapi"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_schema()
{
    last_command="hlmdsrv_schema"

    command_aliases=()

    commands=()
    commands+=("batch")
    commands+=("job")
    commands+=("manifest")
    commands+=("openapi")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_selection_delete()
{
    last_command="hlmdsrv_selection_delete"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_selection_export-index()
{
    last_command="hlmdsrv_selection_export-index"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_selection_list()
{
    last_command="hlmdsrv_selection_list"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_selection_resolve()
{
    last_command="hlmdsrv_selection_resolve"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--target=")
    two_word_flags+=("--target")
    local_nonpersistent_flags+=("--target")
    local_nonpersistent_flags+=("--target=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_selection_save()
{
    last_command="hlmdsrv_selection_save"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--description=")
    two_word_flags+=("--description")
    local_nonpersistent_flags+=("--description")
    local_nonpersistent_flags+=("--description=")
    flags+=("--expr=")
    two_word_flags+=("--expr")
    local_nonpersistent_flags+=("--expr")
    local_nonpersistent_flags+=("--expr=")
    flags+=("--expression=")
    two_word_flags+=("--expression")
    local_nonpersistent_flags+=("--expression")
    local_nonpersistent_flags+=("--expression=")
    flags+=("--id=")
    two_word_flags+=("--id")
    local_nonpersistent_flags+=("--id")
    local_nonpersistent_flags+=("--id=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--kind=")
    two_word_flags+=("--kind")
    local_nonpersistent_flags+=("--kind")
    local_nonpersistent_flags+=("--kind=")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_selection()
{
    last_command="hlmdsrv_selection"

    command_aliases=()

    commands=()
    commands+=("delete")
    commands+=("export-index")
    commands+=("list")
    commands+=("resolve")
    commands+=("save")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_self-test()
{
    last_command="hlmdsrv_self-test"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--backend=")
    two_word_flags+=("--backend")
    local_nonpersistent_flags+=("--backend")
    local_nonpersistent_flags+=("--backend=")
    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--frames=")
    two_word_flags+=("--frames")
    local_nonpersistent_flags+=("--frames")
    local_nonpersistent_flags+=("--frames=")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--out=")
    two_word_flags+=("--out")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    flags+=("--out-dir=")
    two_word_flags+=("--out-dir")
    local_nonpersistent_flags+=("--out-dir")
    local_nonpersistent_flags+=("--out-dir=")
    flags+=("--quickstart")
    local_nonpersistent_flags+=("--quickstart")
    flags+=("--require-gromacs")
    local_nonpersistent_flags+=("--require-gromacs")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_serve_smoke()
{
    last_command="hlmdsrv_serve_smoke"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--allow-host=")
    two_word_flags+=("--allow-host")
    local_nonpersistent_flags+=("--allow-host")
    local_nonpersistent_flags+=("--allow-host=")
    flags+=("--allow-path=")
    two_word_flags+=("--allow-path")
    local_nonpersistent_flags+=("--allow-path")
    local_nonpersistent_flags+=("--allow-path=")
    flags+=("--auth-token=")
    two_word_flags+=("--auth-token")
    local_nonpersistent_flags+=("--auth-token")
    local_nonpersistent_flags+=("--auth-token=")
    flags+=("--backend=")
    two_word_flags+=("--backend")
    local_nonpersistent_flags+=("--backend")
    local_nonpersistent_flags+=("--backend=")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--job-prune-on-start")
    local_nonpersistent_flags+=("--job-prune-on-start")
    flags+=("--job-timeout=")
    two_word_flags+=("--job-timeout")
    local_nonpersistent_flags+=("--job-timeout")
    local_nonpersistent_flags+=("--job-timeout=")
    flags+=("--job-ttl=")
    two_word_flags+=("--job-ttl")
    local_nonpersistent_flags+=("--job-ttl")
    local_nonpersistent_flags+=("--job-ttl=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--log-requests")
    local_nonpersistent_flags+=("--log-requests")
    flags+=("--max-atoms=")
    two_word_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms=")
    flags+=("--max-chunk-bytes=")
    two_word_flags+=("--max-chunk-bytes")
    local_nonpersistent_flags+=("--max-chunk-bytes")
    local_nonpersistent_flags+=("--max-chunk-bytes=")
    flags+=("--max-frame-range=")
    two_word_flags+=("--max-frame-range")
    local_nonpersistent_flags+=("--max-frame-range")
    local_nonpersistent_flags+=("--max-frame-range=")
    flags+=("--max-frames=")
    two_word_flags+=("--max-frames")
    local_nonpersistent_flags+=("--max-frames")
    local_nonpersistent_flags+=("--max-frames=")
    flags+=("--max-queue=")
    two_word_flags+=("--max-queue")
    local_nonpersistent_flags+=("--max-queue")
    local_nonpersistent_flags+=("--max-queue=")
    flags+=("--read-only")
    local_nonpersistent_flags+=("--read-only")
    flags+=("--request-timeout=")
    two_word_flags+=("--request-timeout")
    local_nonpersistent_flags+=("--request-timeout")
    local_nonpersistent_flags+=("--request-timeout=")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--workers=")
    two_word_flags+=("--workers")
    local_nonpersistent_flags+=("--workers")
    local_nonpersistent_flags+=("--workers=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_serve()
{
    last_command="hlmdsrv_serve"

    command_aliases=()

    commands=()
    commands+=("smoke")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--allow-host=")
    two_word_flags+=("--allow-host")
    local_nonpersistent_flags+=("--allow-host")
    local_nonpersistent_flags+=("--allow-host=")
    flags+=("--allow-path=")
    two_word_flags+=("--allow-path")
    local_nonpersistent_flags+=("--allow-path")
    local_nonpersistent_flags+=("--allow-path=")
    flags+=("--auth-token=")
    two_word_flags+=("--auth-token")
    local_nonpersistent_flags+=("--auth-token")
    local_nonpersistent_flags+=("--auth-token=")
    flags+=("--backend=")
    two_word_flags+=("--backend")
    local_nonpersistent_flags+=("--backend")
    local_nonpersistent_flags+=("--backend=")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--host=")
    two_word_flags+=("--host")
    local_nonpersistent_flags+=("--host")
    local_nonpersistent_flags+=("--host=")
    flags+=("--job-prune-on-start")
    local_nonpersistent_flags+=("--job-prune-on-start")
    flags+=("--job-timeout=")
    two_word_flags+=("--job-timeout")
    local_nonpersistent_flags+=("--job-timeout")
    local_nonpersistent_flags+=("--job-timeout=")
    flags+=("--job-ttl=")
    two_word_flags+=("--job-ttl")
    local_nonpersistent_flags+=("--job-ttl")
    local_nonpersistent_flags+=("--job-ttl=")
    flags+=("--log-requests")
    local_nonpersistent_flags+=("--log-requests")
    flags+=("--max-atoms=")
    two_word_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms=")
    flags+=("--max-chunk-bytes=")
    two_word_flags+=("--max-chunk-bytes")
    local_nonpersistent_flags+=("--max-chunk-bytes")
    local_nonpersistent_flags+=("--max-chunk-bytes=")
    flags+=("--max-frame-range=")
    two_word_flags+=("--max-frame-range")
    local_nonpersistent_flags+=("--max-frame-range")
    local_nonpersistent_flags+=("--max-frame-range=")
    flags+=("--max-frames=")
    two_word_flags+=("--max-frames")
    local_nonpersistent_flags+=("--max-frames")
    local_nonpersistent_flags+=("--max-frames=")
    flags+=("--max-queue=")
    two_word_flags+=("--max-queue")
    local_nonpersistent_flags+=("--max-queue")
    local_nonpersistent_flags+=("--max-queue=")
    flags+=("--port=")
    two_word_flags+=("--port")
    local_nonpersistent_flags+=("--port")
    local_nonpersistent_flags+=("--port=")
    flags+=("--read-only")
    local_nonpersistent_flags+=("--read-only")
    flags+=("--request-timeout=")
    two_word_flags+=("--request-timeout")
    local_nonpersistent_flags+=("--request-timeout")
    local_nonpersistent_flags+=("--request-timeout=")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--workers=")
    two_word_flags+=("--workers")
    local_nonpersistent_flags+=("--workers")
    local_nonpersistent_flags+=("--workers=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_session_list()
{
    last_command="hlmdsrv_session_list"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_session_publish()
{
    last_command="hlmdsrv_session_publish"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--dataset=")
    two_word_flags+=("--dataset")
    local_nonpersistent_flags+=("--dataset")
    local_nonpersistent_flags+=("--dataset=")
    flags+=("--description=")
    two_word_flags+=("--description")
    local_nonpersistent_flags+=("--description")
    local_nonpersistent_flags+=("--description=")
    flags+=("--file=")
    two_word_flags+=("--file")
    local_nonpersistent_flags+=("--file")
    local_nonpersistent_flags+=("--file=")
    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--id=")
    two_word_flags+=("--id")
    local_nonpersistent_flags+=("--id")
    local_nonpersistent_flags+=("--id=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--name=")
    two_word_flags+=("--name")
    local_nonpersistent_flags+=("--name")
    local_nonpersistent_flags+=("--name=")
    flags+=("--source=")
    two_word_flags+=("--source")
    local_nonpersistent_flags+=("--source")
    local_nonpersistent_flags+=("--source=")
    flags+=("--sticky")
    local_nonpersistent_flags+=("--sticky")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--version=")
    two_word_flags+=("--version")
    local_nonpersistent_flags+=("--version")
    local_nonpersistent_flags+=("--version=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_flag+=("--dataset=")
    must_have_one_flag+=("--file=")
    must_have_one_flag+=("--id=")
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_session()
{
    last_command="hlmdsrv_session"

    command_aliases=()

    commands=()
    commands+=("list")
    commands+=("publish")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_store_doctor()
{
    last_command="hlmdsrv_store_doctor"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--init")
    local_nonpersistent_flags+=("--init")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--strict")
    local_nonpersistent_flags+=("--strict")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_store()
{
    last_command="hlmdsrv_store"

    command_aliases=()

    commands=()
    commands+=("doctor")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_unpack()
{
    last_command="hlmdsrv_unpack"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_validate()
{
    last_command="hlmdsrv_validate"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--backend=")
    two_word_flags+=("--backend")
    local_nonpersistent_flags+=("--backend")
    local_nonpersistent_flags+=("--backend=")
    flags+=("--deep")
    local_nonpersistent_flags+=("--deep")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--strict")
    local_nonpersistent_flags+=("--strict")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_version()
{
    last_command="hlmdsrv_version"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_visualize()
{
    last_command="hlmdsrv_visualize"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--background=")
    two_word_flags+=("--background")
    local_nonpersistent_flags+=("--background")
    local_nonpersistent_flags+=("--background=")
    flags+=("--color=")
    two_word_flags+=("--color")
    local_nonpersistent_flags+=("--color")
    local_nonpersistent_flags+=("--color=")
    flags+=("--component=")
    two_word_flags+=("--component")
    local_nonpersistent_flags+=("--component")
    local_nonpersistent_flags+=("--component=")
    flags+=("--focus=")
    two_word_flags+=("--focus")
    local_nonpersistent_flags+=("--focus")
    local_nonpersistent_flags+=("--focus=")
    flags+=("--frame=")
    two_word_flags+=("--frame")
    local_nonpersistent_flags+=("--frame")
    local_nonpersistent_flags+=("--frame=")
    flags+=("--gmx-command=")
    two_word_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command")
    local_nonpersistent_flags+=("--gmx-command=")
    flags+=("--include-selections")
    local_nonpersistent_flags+=("--include-selections")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--repr=")
    two_word_flags+=("--repr")
    local_nonpersistent_flags+=("--repr")
    local_nonpersistent_flags+=("--repr=")
    flags+=("--selection=")
    two_word_flags+=("--selection")
    local_nonpersistent_flags+=("--selection")
    local_nonpersistent_flags+=("--selection=")
    flags+=("--store=")
    two_word_flags+=("--store")
    local_nonpersistent_flags+=("--store")
    local_nonpersistent_flags+=("--store=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_hlmdsrv_root_command()
{
    last_command="hlmdsrv"

    command_aliases=()

    commands=()
    commands+=("analyze")
    if [[ -z "${BASH_VERSION:-}" || "${BASH_VERSINFO[0]:-}" -gt 3 ]]; then
        command_aliases+=("analysis")
        aliashash["analysis"]="analyze"
    fi
    commands+=("batch")
    commands+=("bench")
    commands+=("capabilities")
    commands+=("compat")
    commands+=("completion")
    commands+=("config")
    commands+=("dataset")
    commands+=("debug")
    commands+=("demo")
    commands+=("docs")
    commands+=("doctor")
    commands+=("explain")
    commands+=("export")
    commands+=("fixtures")
    commands+=("frames")
    if [[ -z "${BASH_VERSION:-}" || "${BASH_VERSINFO[0]:-}" -gt 3 ]]; then
        command_aliases+=("trajectory")
        aliashash["trajectory"]="frames"
    fi
    commands+=("gromacs")
    commands+=("index")
    commands+=("ingest")
    commands+=("init")
    commands+=("install")
    commands+=("jobs")
    commands+=("list")
    commands+=("pack")
    if [[ -z "${BASH_VERSION:-}" || "${BASH_VERSINFO[0]:-}" -gt 3 ]]; then
        command_aliases+=("archive")
        aliashash["archive"]="pack"
    fi
    commands+=("probe")
    commands+=("publish")
    commands+=("quickstart")
    commands+=("run")
    commands+=("schema")
    commands+=("selection")
    commands+=("self-test")
    commands+=("serve")
    if [[ -z "${BASH_VERSION:-}" || "${BASH_VERSINFO[0]:-}" -gt 3 ]]; then
        command_aliases+=("server")
        aliashash["server"]="serve"
    fi
    commands+=("session")
    commands+=("store")
    commands+=("unpack")
    if [[ -z "${BASH_VERSION:-}" || "${BASH_VERSINFO[0]:-}" -gt 3 ]]; then
        command_aliases+=("restore")
        aliashash["restore"]="unpack"
    fi
    commands+=("validate")
    commands+=("version")
    commands+=("visualize")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--profile=")
    two_word_flags+=("--profile")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

__start_hlmdsrv()
{
    local cur prev words cword split
    declare -A flaghash 2>/dev/null || :
    declare -A aliashash 2>/dev/null || :
    if declare -F _init_completion >/dev/null 2>&1; then
        _init_completion -s || return
    else
        __hlmdsrv_init_completion -n "=" || return
    fi

    local c=0
    local flag_parsing_disabled=
    local flags=()
    local two_word_flags=()
    local local_nonpersistent_flags=()
    local flags_with_completion=()
    local flags_completion=()
    local commands=("hlmdsrv")
    local command_aliases=()
    local must_have_one_flag=()
    local must_have_one_noun=()
    local has_completion_function=""
    local last_command=""
    local nouns=()
    local noun_aliases=()

    __hlmdsrv_handle_word
}

if [[ $(type -t compopt) = "builtin" ]]; then
    complete -o default -F __start_hlmdsrv hlmdsrv
else
    complete -o default -o nospace -F __start_hlmdsrv hlmdsrv
fi

# ex: ts=4 sw=4 et filetype=sh
