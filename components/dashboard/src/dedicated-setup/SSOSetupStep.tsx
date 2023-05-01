/**
 * Copyright (c) 2023 Gitpod GmbH. All rights reserved.
 * Licensed under the GNU Affero General Public License (AGPL).
 * See License.AGPL.txt in the project root for license information.
 */

import { FC, useCallback, useReducer } from "react";
import { Button } from "../components/Button";
import { Heading1, Subheading } from "../components/typography/headings";
import { SetupLayout } from "./SetupLayout";
import check from "../images/check.svg";
import Tooltip from "../components/Tooltip";
import { SSOConfigForm, isValid, ssoConfigReducer, useSaveSSOConfig } from "./SSOConfigForm";
import Alert from "../components/Alert";
import { useToast } from "../components/toasts/Toasts";
import { OIDCClientConfig } from "@gitpod/public-api/lib/gitpod/experimental/v1/oidc_pb";

type Props = {
    config?: OIDCClientConfig;
    onComplete: () => void;
};
export const SSOSetupStep: FC<Props> = ({ config, onComplete }) => {
    const { toast } = useToast();

    const [ssoConfig, dispatch] = useReducer(ssoConfigReducer, {
        id: config?.id ?? "",
        issuer: config?.oidcConfig?.issuer ?? "",
        clientId: config?.oauth2Config?.clientId ?? "",
        clientSecret: config?.oauth2Config?.clientSecret ?? "",
    });
    const configIsValid = isValid(ssoConfig);

    const { save, isLoading, isError, error } = useSaveSSOConfig();

    const handleVerify = useCallback(async () => {
        try {
            const newConfig = await save(ssoConfig);
            console.log("newConfig", newConfig);
            toast("Your SSO configuration was saved");
            // TODO: launch login flow to verify the config

            // TODO: This will happen after login flow was complete
            onComplete();
        } catch (e) {
            console.error(e);
        }
    }, [onComplete, save, ssoConfig, toast]);

    return (
        <SetupLayout>
            <div className="flex flex-row space-x-2 mb-4">
                <Tooltip content="Naming your Organization">
                    <div className="w-5 h-5 bg-green-600 rounded-full flex justify-center items-center text-color-white">
                        <img src={check} width={15} height={15} alt="checkmark" />
                    </div>
                </Tooltip>
                <div className="w-5 h-5 bg-gray-400 rounded-full" />
            </div>

            <div className="mb-10">
                <Heading1>Configure single sign-on</Heading1>
                <Subheading>
                    {/* TODO: Find what link we want to use here */}
                    Enable single sign-on for your organization using the OpenID Connect (OIDC) standard.{" "}
                    <a href="https://gitpod.io" target="_blank" rel="noreferrer noopener">
                        Learn more
                    </a>
                </Subheading>
            </div>
            {isError && <Alert type="danger">{error?.message}</Alert>}

            <SSOConfigForm config={ssoConfig} onChange={dispatch} />

            <div className="mt-6">
                <Button size="block" onClick={handleVerify} disabled={!configIsValid} loading={isLoading}>
                    Verify SSO Configuration
                </Button>
            </div>
        </SetupLayout>
    );
};