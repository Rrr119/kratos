import { faker } from "@faker-js/faker"
import { CDPSession, expect, Page } from "@playwright/test"
import { OryKratosConfiguration } from "../../../../shared/config"
import { getSession } from "../../../actions/session"
import { test } from "../../../fixtures"
import { toConfig } from "../../../lib/helper"
import { LoginPage } from "../../../models/elements/login"

type DeviceCase = {
  name: string
  attachment: "platform" | "cross-platform"
  transport: "internal" | "usb" | "cable"
}

const devices: DeviceCase[] = [
  {
    name: "platform-built-in",
    attachment: "platform",
    transport: "internal",
  },
  {
    name: "roaming-usb-key",
    attachment: "cross-platform",
    transport: "usb",
  },
  {
    name: "hybrid-phone",
    attachment: "cross-platform",
    transport: "cable",
  },
]

async function toggleAutomaticPresenceSimulation(
  cdpSession: CDPSession,
  authenticatorId: string,
  enabled: boolean,
) {
  await cdpSession.send("WebAuthn.setAutomaticPresenceSimulation", {
    authenticatorId,
    enabled,
  })
}

async function registerWithDevicePasskey(
  page: Page,
  cdpSession: CDPSession,
  config: OryKratosConfiguration,
  authenticatorId: string,
) {
  return test.step("register passkey on device authenticator", async () => {
    const identifier = faker.internet.email()

    await page.goto("/registration")
    await page.locator('input[name="traits.email"]').fill(identifier)
    await page
      .locator('input[name="traits.website"]')
      .fill(faker.internet.url())
    await page.locator("button[name=method][value=profile]").click()

    await toggleAutomaticPresenceSimulation(cdpSession, authenticatorId, true)
    await page.locator("button[name=passkey_register_trigger]").click()
    await toggleAutomaticPresenceSimulation(cdpSession, authenticatorId, false)

    await page.waitForURL(
      new RegExp(config.selfservice.default_browser_return_url),
    )
    return identifier
  })
}

for (const device of devices) {
  test.describe(`passkey full ceremony on ${device.name}`, () => {
    test.use({
      configOverride: toConfig({
        mitigateEnumeration: false,
        style: "identifier_first",
        selfservice: {
          methods: {
            passkey: {
              enabled: true,
              config: {
                rp: {
                  display_name: "ORY",
                  id: "localhost",
                  origins: ["http://localhost:4455"],
                },
                authenticator_selection: {
                  attachment: device.attachment,
                  resident_key: "required",
                  user_verification: "required",
                },
              },
            },
          },
          flows: {
            registration: {
              after: {
                passkey: {
                  hooks: [{ hook: "session" }],
                },
              },
            },
          },
        },
      }),
      virtualAuthenticatorOptions: {
        protocol: "ctap2",
        transport: device.transport,
        automaticPresenceSimulation: false,
        hasResidentKey: true,
        hasUserVerification: true,
        isUserVerified: true,
      },
    })

    test("registration and verification both succeed", async ({
      config,
      page,
      pageCDPSession,
      virtualAuthenticator,
      kratosPublicURL,
    }) => {
      const identifier = await registerWithDevicePasskey(
        page,
        pageCDPSession,
        config,
        virtualAuthenticator.authenticatorId,
      )

      await expect(
        getSession(page.request, kratosPublicURL),
      ).resolves.toMatchObject({
        active: true,
        identity: { traits: { email: identifier } },
      })

      await page.context().clearCookies()

      const loginPage = new LoginPage(page, config)
      await loginPage.open()
      await loginPage.submitIdentifierFirst(identifier)

      const passkeyLoginTrigger = page.locator(
        "button[name=passkey_login_trigger]",
      )
      await passkeyLoginTrigger.waitFor()
      await page.waitForLoadState("load")

      await toggleAutomaticPresenceSimulation(
        pageCDPSession,
        virtualAuthenticator.authenticatorId,
        true,
      )
      await passkeyLoginTrigger.click()

      await page.waitForURL(
        new RegExp(config.selfservice.default_browser_return_url),
      )

      await expect(
        getSession(page.request, kratosPublicURL),
      ).resolves.toMatchObject({
        active: true,
        identity: { traits: { email: identifier } },
      })
    })
  })
}
